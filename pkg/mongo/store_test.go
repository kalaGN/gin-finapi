package mongo

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
	"github.com/kalaGN/gin-finapi/pkg/utils"
	driver "go.mongodb.org/mongo-driver/mongo"
)

func TestStore_UsesInstanceSnapshotNotGlobalConfig(t *testing.T) {
	for _, name := range []string{"first", "second"} {
		cfg := config.MongoDBConfig{DSN: "mongodb://" + name, Database: name, Collection: "records", Timeout: "2s", MaxPool: 100, MinPool: 10}
		store, err := NewStore(cfg)
		if err != nil {
			t.Fatal(err)
		}
		cfg.Database = "changed"
		if store.cfg.DSN != "mongodb://"+name {
			t.Fatal("instance DSN changed")
		}
		store.clients.connect = func(context.Context) (*driver.Client, error) { return &driver.Client{}, nil }
		store.clients.disconnect = func(context.Context, *driver.Client) error { return nil }
		t.Cleanup(func() {
			if err := store.Close(context.Background()); err != nil {
				t.Error(err)
			}
		})
		store.find = func(ctx context.Context, _ *driver.Client, database, collection, phone string) ([]byte, error) {
			if database != name || collection != "records" || phone != "lookup" {
				t.Fatal("instance config changed")
			}
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 2*time.Second {
				t.Fatal("instance timeout missing")
			}
			return gzipBytes(t, []byte("1,2")), nil
		}
		got, err := store.Query(context.Background(), "lookup", map[string]int{"a": 0, "b": 1})
		if err != nil || got["a"] != 1 || got["b"] != 2 {
			t.Fatalf("result=%v err=%v", got, err)
		}
	}
}

func TestStore_CloseDoesNotConnectAndPreventsReopening(t *testing.T) {
	store, err := NewStore(config.MongoDBConfig{DSN: "mongodb://test", Database: "test", MaxPool: 100})
	if err != nil {
		t.Fatal(err)
	}
	store.clients.connect = func(context.Context) (*driver.Client, error) { t.Fatal("unexpected connect"); return nil, nil }
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Query(context.Background(), "lookup", map[string]int{"a": 0}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestStore_RejectsInvalidConfigAndMissingMapping(t *testing.T) {
	for _, cfg := range []config.MongoDBConfig{
		{}, {DSN: "mongodb://test", Database: "test", MaxPool: 1, MinPool: 2},
		{DSN: "mongodb://test", Database: "test", MaxPool: 1, MinPool: -1},
	} {
		if _, err := NewStore(cfg); !errors.Is(err, ErrUnavailable) {
			t.Fatalf("err=%v", err)
		}
	}
	store, err := NewStore(config.MongoDBConfig{DSN: "mongodb://test", Database: "test", MaxPool: 1})
	if err != nil {
		t.Fatal(err)
	}
	store.clients.connect = func(context.Context) (*driver.Client, error) { t.Fatal("mapping failure connected"); return nil, nil }
	if _, err := store.Query(context.Background(), "lookup", nil); !errors.Is(err, ErrInvalidData) {
		t.Fatalf("err=%v", err)
	}
}

func TestStoreQueryFailureBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name                string
		connectErr, findErr error
		badPayload          bool
		want                error
	}{
		{"初始化故障", ErrUnavailable, nil, false, ErrUnavailable},
		{"未查得", nil, ErrNotFound, false, ErrNotFound},
		{"查询超时", nil, context.DeadlineExceeded, false, context.DeadlineExceeded},
		{"查询取消", nil, context.Canceled, false, context.Canceled},
		{"查询不可用", nil, ErrUnavailable, false, ErrUnavailable},
		{"坏gzip数据", nil, nil, true, ErrInvalidData},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store, err := NewStore(config.MongoDBConfig{DSN: "synthetic-invalid-dsn", Database: "synthetic", MaxPool: 1})
			if err != nil {
				t.Fatal(err)
			}
			store.clients.connect = func(context.Context) (*driver.Client, error) { return &driver.Client{}, tc.connectErr }
			store.clients.disconnect = func(context.Context, *driver.Client) error { return nil }
			finds := 0
			store.find = func(ctx context.Context, _ *driver.Client, _ string, collection, phone string) ([]byte, error) {
				finds++
				if collection != "data_20251101_0" || phone != "lookup" {
					t.Error("默认集合或查询条件改变")
				}
				if _, ok := ctx.Deadline(); !ok {
					t.Error("查询缺少预算")
				}
				if tc.badPayload {
					return []byte("invalid-gzip"), nil
				}
				return nil, tc.findErr
			}
			data, err := store.Query(context.Background(), "lookup", map[string]int{"a": 0})
			if !errors.Is(err, tc.want) || data != nil {
				t.Fatal("Mongo错误语义或结果改变")
			}
			wantFinds := 1
			if tc.connectErr != nil {
				wantFinds = 0
			}
			if finds != wantFinds {
				t.Fatal("初始化失败后仍执行查询")
			}
			if err := store.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStoreQueryTimingStages(t *testing.T) {
	stages := []string{"mongo_start", "mongo_client", "mongo_find", "gzip", "csv_map"}
	for _, tc := range []struct {
		name                string
		connectErr, findErr error
		payload             string
		missingMap, badGzip bool
		want                error
		completed           int
	}{
		{name: "成功", payload: "7", completed: 5},
		{name: "缺少映射", missingMap: true, want: ErrInvalidData},
		{name: "初始化失败", connectErr: ErrUnavailable, want: ErrUnavailable, completed: 2},
		{name: "初始化超时", connectErr: context.DeadlineExceeded, want: context.DeadlineExceeded, completed: 2},
		{name: "初始化取消", connectErr: context.Canceled, want: context.Canceled, completed: 2},
		{name: "未查得", findErr: ErrNotFound, want: ErrNotFound, completed: 3},
		{name: "查询故障", findErr: ErrUnavailable, want: ErrUnavailable, completed: 3},
		{name: "查询超时", findErr: context.DeadlineExceeded, want: context.DeadlineExceeded, completed: 3},
		{name: "解压失败", badGzip: true, want: ErrInvalidData, completed: 4},
		{name: "CSV错误", payload: "not-integer", want: ErrInvalidData, completed: 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := utils.StartTiming(context.Background())
			store, err := NewStore(config.MongoDBConfig{DSN: "synthetic-invalid-dsn", Database: "synthetic", MaxPool: 1})
			if err != nil {
				t.Fatal(err)
			}
			store.clients.connect = func(context.Context) (*driver.Client, error) { return &driver.Client{}, tc.connectErr }
			store.clients.disconnect = func(context.Context, *driver.Client) error { return nil }
			store.find = func(actual context.Context, _ *driver.Client, _, _, _ string) ([]byte, error) {
				points := utils.TimingSnapshot(actual)
				if _, ok := points["mongo_client"]; !ok {
					t.Error("Find前未标记客户端获取结束")
				}
				if _, ok := points["mongo_find"]; ok {
					t.Error("Find未返回就记录结束")
				}
				if tc.findErr != nil {
					return nil, tc.findErr
				}
				if tc.badGzip {
					return []byte("bad-gzip"), nil
				}
				return gzipBytes(t, []byte(tc.payload)), nil
			}
			mapping := map[string]int{"a": 0}
			if tc.missingMap {
				mapping = nil
			}
			result, err := store.Query(ctx, "synthetic", mapping)
			if !errors.Is(err, tc.want) {
				t.Fatalf("错误分类改变=%v", err)
			}
			if tc.want == nil && result["a"] != 7 {
				t.Fatalf("查询结果=%v", result)
			}
			points := utils.TimingSnapshot(ctx)
			if len(points) != tc.completed+1 {
				t.Fatalf("节点集合=%v", points)
			}
			previous := int64(0)
			for i, name := range stages {
				value, ok := points[name]
				if ok != (i < tc.completed) {
					t.Fatalf("节点%s执行状态错误=%v", name, points)
				}
				if ok && value < previous {
					t.Fatalf("累计节点倒序=%v", points)
				}
				if ok {
					previous = value
				}
			}
			if err := store.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestStoreQueryTimeoutIncludesInitialization(t *testing.T) {
	store, err := NewStore(config.MongoDBConfig{DSN: "synthetic-invalid-dsn", Database: "synthetic", MaxPool: 1, Timeout: "30ms"})
	if err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	t.Cleanup(unblock)
	started := make(chan struct{})
	var finds atomic.Int32
	store.clients.connect = func(ctx context.Context) (*driver.Client, error) {
		close(started)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-release:
			return &driver.Client{}, nil
		}
	}
	store.clients.disconnect = func(context.Context, *driver.Client) error { return nil }
	store.find = func(context.Context, *driver.Client, string, string, string) ([]byte, error) {
		finds.Add(1)
		return nil, ErrNotFound
	}
	result := make(chan error, 1)
	go func() { _, err := store.Query(context.Background(), "lookup", map[string]int{"a": 0}); result <- err }()
	<-started
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("冷启动应返回预算超时，实际=%v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Error("查询预算未覆盖初始化")
	}
	unblock()
	if err := store.Close(context.Background()); err != nil {
		t.Error(err)
	}
	if finds.Load() != 0 {
		t.Error("初始化超时后仍执行查询")
	}
}

func TestStoreQuerySharesBudgetWithInitialization(t *testing.T) {
	store, err := NewStore(config.MongoDBConfig{DSN: "synthetic-invalid-dsn", Database: "synthetic", MaxPool: 1, Timeout: "2s"})
	if err != nil {
		t.Fatal(err)
	}
	var initDeadline time.Time
	store.clients.connect = func(ctx context.Context) (*driver.Client, error) {
		var ok bool
		initDeadline, ok = ctx.Deadline()
		if !ok {
			t.Error("初始化未获得预算")
		}
		return &driver.Client{}, nil
	}
	store.clients.disconnect = func(context.Context, *driver.Client) error { return nil }
	store.find = func(ctx context.Context, _ *driver.Client, _, _, _ string) ([]byte, error) {
		deadline, ok := ctx.Deadline()
		if !ok || initDeadline.IsZero() || deadline.After(initDeadline) {
			t.Error("连接完成后重新计算了查询预算")
		}
		return gzipBytes(t, []byte("7")), nil
	}
	if value, err := store.Query(context.Background(), "lookup", map[string]int{"a": 0}); err != nil || value["a"] != 7 {
		t.Error("正常查询结果改变")
	}
	if err := store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestStoreQueryPreservesCallerCancellationAndDeadline(t *testing.T) {
	for _, canceled := range []bool{true, false} {
		t.Run(map[bool]string{true: "已取消不连接", false: "上游更早截止"}[canceled], func(t *testing.T) {
			store, err := NewStore(config.MongoDBConfig{DSN: "synthetic-invalid-dsn", Database: "synthetic", MaxPool: 1, Timeout: "2s"})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			if canceled {
				cancel()
			}
			store.clients.connect = func(context.Context) (*driver.Client, error) {
				if canceled {
					t.Error("已取消请求仍连接")
				}
				return &driver.Client{}, nil
			}
			store.clients.disconnect = func(context.Context, *driver.Client) error { return nil }
			store.find = func(queryCtx context.Context, _ *driver.Client, _, _, _ string) ([]byte, error) {
				if canceled {
					t.Error("已取消请求仍查询")
				}
				want, _ := ctx.Deadline()
				got, ok := queryCtx.Deadline()
				if !ok || !got.Equal(want) {
					t.Error("未保留上游更早截止时间")
				}
				return gzipBytes(t, []byte("7")), nil
			}
			_, err = store.Query(ctx, "lookup", map[string]int{"a": 0})
			if canceled && !errors.Is(err, context.Canceled) {
				t.Fatalf("取消错误=%v", err)
			}
			if !canceled && err != nil {
				t.Fatal(err)
			}
			if err := store.Close(context.Background()); err != nil {
				t.Fatal(err)
			}
		})
	}
}
