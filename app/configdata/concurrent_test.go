package configdata

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/pkg/config"
)

func TestReaderConcurrentRefreshAndMappingIsolation(t *testing.T) {
	var revision atomic.Int64
	r := testReader(t, Sources{Maps: []Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) {
		return map[string]int{"value": int(revision.Load())}, true, nil
	}}}, config.CacheConfig{Concurrency: 4})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := r.LoadProductMap(ctx, 10001); err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 100; i++ {
				v, err := r.LoadProductMap(ctx, 10001)
				if err != nil {
					t.Error(err)
					return
				}
				if v["value"] < 0 || v["value"] > 100 {
					t.Error("caller mutation leaked into cache")
					return
				}
				v["value"] = -1
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-start
		for i := 1; i <= 100; i++ {
			revision.Store(int64(i))
			stats := r.Refresh(ctx)
			if stats.Failed != 0 {
				t.Errorf("refresh failures: %d", stats.Failed)
				return
			}
		}
	}()
	close(start)
	wg.Wait()
	v, err := r.LoadProductMap(ctx, 10001)
	if err != nil || v["value"] != 100 {
		t.Fatalf("final mapping=%v err=%v", v, err)
	}
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadProductMap(ctx, 10001); err == nil {
		t.Fatal("closed reader accepted a cached read")
	}
}
