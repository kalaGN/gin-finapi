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
