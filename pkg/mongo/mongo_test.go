package mongo

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	drivermongo "go.mongodb.org/mongo-driver/mongo"
)

func TestClientManager_RetriesAfterInitializationFailure(t *testing.T) {
	wantClient := &drivermongo.Client{}
	var calls atomic.Int32
	manager := clientManager{
		connect: func(context.Context) (*drivermongo.Client, error) {
			if calls.Add(1) == 1 {
				return nil, errors.New("initial connection failed")
			}
			return wantClient, nil
		},
	}

	if _, err := manager.get(context.Background()); err == nil {
		t.Fatal("first get() error = nil, want initialization failure")
	}
	gotClient, err := manager.get(context.Background())
	if err != nil {
		t.Fatalf("second get() error = %v", err)
	}
	if gotClient != wantClient {
		t.Fatalf("second get() client = %p, want %p", gotClient, wantClient)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("connect calls = %d, want 2", got)
	}
}

func TestClientManager_ConcurrentInitializationPublishesOneClient(t *testing.T) {
	wantClient := &drivermongo.Client{}
	var calls atomic.Int32
	manager := clientManager{
		connect: func(context.Context) (*drivermongo.Client, error) {
			calls.Add(1)
			return wantClient, nil
		},
	}

	const workers = 100
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			client, err := manager.get(context.Background())
			if err != nil {
				errs <- err
				return
			}
			if client != wantClient {
				errs <- errors.New("unexpected client instance")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("connect calls = %d, want 1", got)
	}
}

func TestClientManager_ClosePreventsReinitialization(t *testing.T) {
	firstClient := &drivermongo.Client{}
	var connectCalls int
	var disconnectCalls int
	var disconnected *drivermongo.Client
	manager := clientManager{
		connect: func(context.Context) (*drivermongo.Client, error) {
			connectCalls++
			return firstClient, nil
		},
		disconnect: func(_ context.Context, client *drivermongo.Client) error {
			disconnected = client
			disconnectCalls++
			return nil
		},
	}

	if _, err := manager.get(context.Background()); err != nil {
		t.Fatalf("first get() error = %v", err)
	}
	if err := manager.close(context.Background()); err != nil {
		t.Fatalf("close() error = %v", err)
	}
	gotClient, err := manager.get(context.Background())
	if !errors.Is(err, ErrUnavailable) || gotClient != nil {
		t.Fatalf("get() after close = %p, %v; want nil, ErrUnavailable", gotClient, err)
	}
	if disconnected != firstClient {
		t.Fatalf("disconnected client = %p, want %p", disconnected, firstClient)
	}
	if err := manager.close(context.Background()); err != nil {
		t.Fatalf("repeated close() error = %v", err)
	}
	if connectCalls != 1 || disconnectCalls != 1 {
		t.Fatalf("connect/disconnect calls = %d/%d, want 1/1", connectCalls, disconnectCalls)
	}
}

func TestClientManagerCanceledWaiterDoesNotWaitForInitialization(t *testing.T) {
	started, release, firstDone := make(chan struct{}), make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	manager := &clientManager{connect: func(context.Context) (*drivermongo.Client, error) {
		close(started)
		<-release
		return &drivermongo.Client{}, nil
	}}
	go func() { defer close(firstDone); _, _ = manager.get(context.Background()) }()
	<-started
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := make(chan error, 1)
	go func() { _, err := manager.get(ctx); result <- err }()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("等待者取消错误=%v", err)
		}
	case <-time.After(150 * time.Millisecond):
		t.Error("取消的等待者仍被初始化阻塞")
	}
	unblock()
	<-firstDone
}

func TestClientManagerCloseDeadlineAndLateCandidate(t *testing.T) {
	for _, cleanupErr := range []error{nil, errors.New("synthetic-disconnect-failed")} {
		t.Run(map[bool]string{false: "清理成功", true: "清理错误保留"}[cleanupErr != nil], func(t *testing.T) {
			started, release, cleaned := make(chan struct{}), make(chan struct{}), make(chan struct{})
			unblock := sync.OnceFunc(func() { close(release) })
			t.Cleanup(unblock)
			var disconnects atomic.Int32
			manager := &clientManager{connect: func(context.Context) (*drivermongo.Client, error) {
				close(started)
				<-release
				return &drivermongo.Client{}, nil
			}, disconnect: func(ctx context.Context, _ *drivermongo.Client) error {
				if ctx.Err() != nil {
					t.Error("候选清理继承了已到期调用Context")
				}
				disconnects.Add(1)
				close(cleaned)
				return cleanupErr
			}}
			firstDone := make(chan error, 1)
			go func() { _, err := manager.get(context.Background()); firstDone <- err }()
			<-started
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			closeDone := make(chan error, 1)
			go func() { closeDone <- manager.close(ctx) }()
			select {
			case err := <-closeDone:
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("关闭应按调用预算返回，实际=%v", err)
				}
			case <-time.After(200 * time.Millisecond):
				t.Error("关闭被网络初始化锁阻塞")
			}
			unblock()
			select {
			case <-cleaned:
			case <-time.After(time.Second):
				t.Fatal("迟到客户端未清理")
			}
			if err := manager.close(context.Background()); !errors.Is(err, cleanupErr) {
				t.Errorf("关闭未保留清理结果：%v", err)
			}
			if disconnects.Load() != 1 {
				t.Error("候选被重复释放")
			}
			if err := <-firstDone; !errors.Is(err, ErrUnavailable) {
				t.Errorf("关闭后初始化仍成功发布：%v", err)
			}
			manager.mu.Lock()
			published := manager.client
			manager.mu.Unlock()
			if published != nil {
				t.Error("关闭后仍保留客户端")
			}
			if _, err := manager.get(context.Background()); !errors.Is(err, ErrUnavailable) {
				t.Error("关闭后允许再次初始化")
			}
		})
	}
}

// observedWaitContext 在进入等待选择时通知测试，避免靠睡眠猜测并发顺序。
type observedWaitContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (ctx *observedWaitContext) Done() <-chan struct{} {
	ctx.once.Do(func() { close(ctx.observed) })
	return ctx.Context.Done()
}

func TestClientManagerFirstCallerCancellationDoesNotCancelSharedInitialization(t *testing.T) {
	started, release := make(chan context.Context, 1), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	want := &drivermongo.Client{}
	var calls atomic.Int32
	manager := &clientManager{connect: func(ctx context.Context) (*drivermongo.Client, error) {
		calls.Add(1)
		started <- ctx
		<-release
		return want, nil
	}}
	firstCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := make(chan error, 1)
	go func() { _, err := manager.get(firstCtx); first <- err }()
	initCtx := <-started
	secondCtx := &observedWaitContext{Context: context.Background(), observed: make(chan struct{})}
	second := make(chan error, 1)
	go func() {
		client, err := manager.get(secondCtx)
		if err == nil && client != want {
			err = errors.New("非共享客户端")
		}
		second <- err
	}()
	<-secondCtx.observed
	cancel()
	if err := <-first; !errors.Is(err, context.Canceled) {
		t.Fatalf("首请求=%v", err)
	}
	if initCtx.Err() != nil {
		t.Fatal("首请求取消了共享初始化")
	}
	unblock()
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatal("重复初始化")
	}
	if err := manager.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientManagerSharedFailureThenRetry(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	failure := errors.New("初始化失败")
	var calls atomic.Int32
	manager := &clientManager{connect: func(context.Context) (*drivermongo.Client, error) {
		if calls.Add(1) == 1 {
			close(started)
			<-release
			return nil, failure
		}
		return &drivermongo.Client{}, nil
	}}
	first, second := make(chan error, 1), make(chan error, 1)
	go func() { _, err := manager.get(context.Background()); first <- err }()
	<-started
	ctx := &observedWaitContext{Context: context.Background(), observed: make(chan struct{})}
	go func() { _, err := manager.get(ctx); second <- err }()
	<-ctx.observed
	unblock()
	for _, result := range []chan error{first, second} {
		if err := <-result; !errors.Is(err, failure) {
			t.Fatalf("同轮错误=%v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("失败等待者启动了新一轮")
	}
	if _, err := manager.get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal("后续请求未重试")
	}
	if err := manager.close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClientManagerInitializationTimeoutCleansLateCandidate(t *testing.T) {
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	var disconnects atomic.Int32
	manager := &clientManager{initTimeout: 20 * time.Millisecond, connect: func(context.Context) (*drivermongo.Client, error) {
		<-release
		return &drivermongo.Client{}, nil
	}, disconnect: func(ctx context.Context, _ *drivermongo.Client) error {
		if ctx.Err() != nil {
			t.Error("清理预算已取消")
		}
		disconnects.Add(1)
		return nil
	}}
	if _, err := manager.get(context.Background()); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("初始化超时=%v", err)
	}
	unblock()
	if err := manager.close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if disconnects.Load() != 1 {
		t.Fatal("迟到客户端未恰好清理一次")
	}
}

func TestClientManagerCloseCancelsInitialization(t *testing.T) {
	started := make(chan struct{})
	manager := &clientManager{connect: func(ctx context.Context) (*drivermongo.Client, error) {
		close(started)
		<-ctx.Done()
		return nil, ctx.Err()
	}}
	done := make(chan error, 1)
	go func() { _, err := manager.get(context.Background()); done <- err }()
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := manager.close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrUnavailable) {
		t.Fatalf("关闭等待者=%v", err)
	}
}

func TestClientManagerPartialAndNilCandidates(t *testing.T) {
	for _, partial := range []bool{false, true} {
		t.Run(map[bool]string{false: "空客户端", true: "部分客户端"}[partial], func(t *testing.T) {
			failure := errors.New("部分连接失败")
			var disconnects atomic.Int32
			manager := &clientManager{connect: func(context.Context) (*drivermongo.Client, error) {
				if partial {
					return &drivermongo.Client{}, failure
				}
				return nil, nil
			}, disconnect: func(context.Context, *drivermongo.Client) error { disconnects.Add(1); return nil }}
			_, err := manager.get(context.Background())
			want := ErrUnavailable
			if partial {
				want = failure
			}
			if !errors.Is(err, want) {
				t.Fatalf("候选错误=%v", err)
			}
			if err := manager.close(context.Background()); err != nil {
				t.Fatal(err)
			}
			if (disconnects.Load() == 1) != partial {
				t.Fatal("候选清理次数错误")
			}
		})
	}
}

func TestClientManagerPublishedClientCloseDeadline(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	failure := errors.New("释放失败")
	var calls atomic.Int32
	manager := &clientManager{connect: func(context.Context) (*drivermongo.Client, error) { return &drivermongo.Client{}, nil }, disconnect: func(ctx context.Context, _ *drivermongo.Client) error {
		calls.Add(1)
		close(started)
		<-release
		if ctx.Err() != nil {
			t.Error("释放继承调用预算")
		}
		return failure
	}}
	if _, err := manager.get(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- manager.close(ctx) }()
	<-started
	if err := <-done; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("关闭预算=%v", err)
	}
	unblock()
	if err := manager.close(context.Background()); !errors.Is(err, failure) {
		t.Fatalf("清理结果=%v", err)
	}
	if calls.Load() != 1 {
		t.Fatal("重复释放")
	}
}

func TestProcessUserData_StrictlyValidatesAllFields(t *testing.T) {
	varList := map[string]int{"a": 0, "b": 1}

	got, err := ProcessUserData(varList, []string{"10", "20"})
	if err != nil {
		t.Fatalf("ProcessUserData() error = %v", err)
	}
	if got["a"] != 10 || got["b"] != 20 {
		t.Fatalf("ProcessUserData() = %#v, want a=10 b=20", got)
	}

	for _, values := range [][]string{
		{"10"},
		{"10", "20", "30"},
		{"10", "invalid"},
		{"10", ""},
	} {
		if _, err := ProcessUserData(varList, values); !errors.Is(err, ErrInvalidData) {
			t.Errorf("ProcessUserData(%q) error = %v, want ErrInvalidData", values, err)
		}
	}
}

func TestDecodeUserData_RejectsCorruptPayload(t *testing.T) {
	if _, err := decodeUserData(context.Background(), []byte("not gzip"), testVariableMapping()); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("decodeUserData(corrupt, testVariableMapping()) error = %v, want ErrInvalidData", err)
	}

	compressed := gzipBytes(t, []byte("1,2,3,4,5"))
	if _, err := decodeUserData(context.Background(), compressed, testVariableMapping()); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("decodeUserData(short payload, testVariableMapping()) error = %v, want ErrInvalidData", err)
	}

	got, err := decodeUserData(context.Background(), gzipBytes(t, []byte("1,2,3,4,5,6")), testVariableMapping())
	if err != nil {
		t.Fatalf("decodeUserData(valid payload, testVariableMapping()) error = %v", err)
	}
	if got["var100001"] != 1 || got["var100006"] != 6 {
		t.Fatalf("decodeUserData(valid payload, testVariableMapping()) = %#v", got)
	}
}

func TestClassifyMongoError(t *testing.T) {
	timeoutErr := classifyMongoError("query", context.DeadlineExceeded)
	if !errors.Is(timeoutErr, context.DeadlineExceeded) {
		t.Fatalf("timeout error = %v, want context.DeadlineExceeded", timeoutErr)
	}
	if errors.Is(timeoutErr, ErrUnavailable) {
		t.Fatalf("timeout error = %v, must not be classified unavailable", timeoutErr)
	}

	unavailableErr := classifyMongoError("query", errors.New("network failure"))
	if !errors.Is(unavailableErr, ErrUnavailable) {
		t.Fatalf("unavailable error = %v, want ErrUnavailable", unavailableErr)
	}
}

func TestMongoTimeout(t *testing.T) {
	cases := []struct {
		raw  string
		want time.Duration
	}{
		{"5s", 5 * time.Second},
		{"1500ms", 1500 * time.Millisecond},
		{"", 5 * time.Second},
		{"0s", 5 * time.Second},
		{"-3s", 5 * time.Second},
		{"nonsense", 5 * time.Second},
	}

	for _, tc := range cases {
		if got := mongoTimeout(tc.raw); got != tc.want {
			t.Errorf("mongoTimeout(%q) = %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func gzipBytes(t *testing.T, data []byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := gzip.NewWriter(&buffer)
	if _, err := writer.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

// testVariableMapping 只供测试使用，不是生产回退配置。
func testVariableMapping() map[string]int {
	return map[string]int{"var100001": 0, "var100002": 1, "var100003": 2, "var100004": 3, "var100005": 4, "var100006": 5}
}
