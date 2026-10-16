package mongo

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
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
