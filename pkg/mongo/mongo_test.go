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
	if _, err := decodeUserData([]byte("not gzip"), testVariableMapping()); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("decodeUserData(corrupt, testVariableMapping()) error = %v, want ErrInvalidData", err)
	}

	compressed := gzipBytes(t, []byte("1,2,3,4,5"))
	if _, err := decodeUserData(compressed, testVariableMapping()); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("decodeUserData(short payload, testVariableMapping()) error = %v, want ErrInvalidData", err)
	}

	got, err := decodeUserData(gzipBytes(t, []byte("1,2,3,4,5,6")), testVariableMapping())
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
