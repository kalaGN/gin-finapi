package configdata

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kalaGN/gin-finapi/app/auth"
	"github.com/kalaGN/gin-finapi/app/services"
	"github.com/kalaGN/gin-finapi/pkg/config"
)

func testReader(t *testing.T, s Sources, cfg config.CacheConfig) *Reader {
	t.Helper()
	if len(s.Credentials) == 0 {
		s.Credentials = []Source[string, auth.Credential]{func(context.Context, string) (auth.Credential, bool, error) {
			return auth.Credential{Version: 1, Secret: "secret", Status: 1}, true, nil
		}}
	}
	if len(s.Products) == 0 {
		s.Products = []Source[ProductKey, auth.ProductAccess]{func(context.Context, ProductKey) (auth.ProductAccess, bool, error) {
			return auth.ProductAccess{Version: 1, Status: 1}, true, nil
		}}
	}
	if len(s.Maps) == 0 {
		s.Maps = []Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) { return map[string]int{"a": 0}, true, nil }}
	}
	r, err := New(s, cfg, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := r.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return r
}

func TestReaderNoAgeTTLAndBusinessExpiry(t *testing.T) {
	calls := 0
	now := time.Unix(100, 0)
	r := testReader(t, Sources{Credentials: []Source[string, auth.Credential]{func(_ context.Context, key string) (auth.Credential, bool, error) {
		calls++
		if key != Digest("key") {
			t.Error("raw API Key used in source")
		}
		return auth.Credential{Version: 1, Secret: "secret", Status: 1, ExpiresAt: 1000}, true, nil
	}}}, config.CacheConfig{})
	r.now = func() time.Time { return now }
	if _, err := r.LoadCredential(context.Background(), "key"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	if _, err := r.LoadCredential(context.Background(), "key"); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(1000, 0)
	if _, err := r.LoadCredential(context.Background(), "key"); !errors.Is(err, auth.ErrInvalid) {
		t.Fatal("expired credential accepted")
	}
	if calls != 1 {
		t.Fatalf("age triggered source reads: %d", calls)
	}
}

func TestReaderRefreshRotationDeletionFailureAndInvalid(t *testing.T) {
	credential := auth.Credential{Version: 1, Secret: "old", Status: 1}
	found := true
	var readErr error
	r := testReader(t, Sources{Credentials: []Source[string, auth.Credential]{func(context.Context, string) (auth.Credential, bool, error) { return credential, found, readErr }}}, config.CacheConfig{})
	ctx := context.Background()
	get := func(want string, wantErr error) {
		t.Helper()
		v, err := r.LoadCredential(ctx, "key")
		if !errors.Is(err, wantErr) || err == nil && v.Secret != want {
			t.Fatalf("credential=%v err=%v", v.Status, err)
		}
	}
	get("old", nil)
	credential.Secret = "new"
	get("old", nil)
	r.Refresh(ctx)
	get("new", nil)
	readErr = errors.New("network")
	if stats := r.Refresh(ctx); stats.Failed != 1 {
		t.Fatal(stats)
	}
	get("new", nil)
	readErr = ErrInvalidRecord
	r.Refresh(ctx)
	get("", auth.ErrUnavailable)
	readErr = nil
	get("new", nil)
	credential.Status = 0
	r.Refresh(ctx)
	get("", auth.ErrInvalid)
	credential.Status = 1
	r.Refresh(ctx)
	get("new", nil)
	found = false
	r.Refresh(ctx)
	get("", auth.ErrInvalid)
}

func TestReaderProductRevocationAndExpiry(t *testing.T) {
	access := auth.ProductAccess{Version: 1, Status: 1, ExpiresAt: 101}
	calls := 0
	now := time.Unix(100, 0)
	r := testReader(t, Sources{Products: []Source[ProductKey, auth.ProductAccess]{func(_ context.Context, key ProductKey) (auth.ProductAccess, bool, error) {
		calls++
		if key != (ProductKey{Digest("key"), 10001}) {
			t.Error("wrong product key")
		}
		return access, true, nil
	}}}, config.CacheConfig{})
	r.now = func() time.Time { return now }
	ctx := context.Background()
	if err := r.CheckProductAccess(ctx, "key", 10001); err != nil {
		t.Fatal(err)
	}
	now = time.Unix(101, 0)
	if err := r.CheckProductAccess(ctx, "key", 10001); !errors.Is(err, auth.ErrProductDenied) {
		t.Fatal("expiry ignored")
	}
	if calls != 1 {
		t.Fatal("expiry triggered source reads")
	}
	access.ExpiresAt = 0
	access.Status = 0
	r.Refresh(ctx)
	if err := r.CheckProductAccess(ctx, "key", 10001); !errors.Is(err, auth.ErrProductDenied) {
		t.Fatal("revocation ignored")
	}
	access.Status = 1
	r.Refresh(ctx)
	if err := r.CheckProductAccess(ctx, "key", 10001); err != nil {
		t.Fatal(err)
	}
}

func TestReaderMappingIsolationAndRefresh(t *testing.T) {
	original := map[string]int{"a": 0}
	found := true
	var readErr error
	r := testReader(t, Sources{Maps: []Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) { return original, found, readErr }}}, config.CacheConfig{})
	ctx := context.Background()
	value, err := r.LoadProductMap(ctx, 10001)
	if err != nil {
		t.Fatal(err)
	}
	value["a"] = 99
	original["a"] = 1
	value, _ = r.LoadProductMap(ctx, 10001)
	if value["a"] != 0 {
		t.Fatal("shared mutable map")
	}
	r.Refresh(ctx)
	value, _ = r.LoadProductMap(ctx, 10001)
	if value["a"] != 1 {
		t.Fatal("refresh did not bypass cache")
	}
	readErr = errors.New("network")
	r.Refresh(ctx)
	if _, err := r.LoadProductMap(ctx, 10001); err != nil {
		t.Fatal("lost last snapshot")
	}
	readErr = ErrInvalidRecord
	r.Refresh(ctx)
	if _, err := r.LoadProductMap(ctx, 10001); !errors.Is(err, services.ErrProductMapping) {
		t.Fatal(err)
	}
	readErr = nil
	found = false
	if _, err := r.LoadProductMap(ctx, 10001); !errors.Is(err, services.ErrProductMapping) {
		t.Fatal(err)
	}
}

func TestSourcesOnlyFallThroughOnMissing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		found  bool
		status int
		err    error
		lower  int
		want   error
	}{
		{"missing", false, 0, nil, 1, nil}, {"enabled", true, 1, nil, 0, nil}, {"disabled", true, 0, nil, 0, auth.ErrInvalid}, {"failed", false, 0, errors.New("storage"), 0, auth.ErrUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			r := testReader(t, Sources{Credentials: []Source[string, auth.Credential]{
				func(context.Context, string) (auth.Credential, bool, error) {
					return auth.Credential{Status: tc.status, Secret: "first"}, tc.found, tc.err
				},
				func(context.Context, string) (auth.Credential, bool, error) {
					calls++
					return auth.Credential{Status: 1, Secret: "second"}, true, nil
				},
			}}, config.CacheConfig{})
			_, err := r.LoadCredential(context.Background(), "key")
			if !errors.Is(err, tc.want) || calls != tc.lower {
				t.Fatalf("err=%v lower=%d", err, calls)
			}
		})
	}
}

func TestCacheLRUAndMissingNotStored(t *testing.T) {
	reads := make(map[int]int)
	r := testReader(t, Sources{Maps: []Source[int, map[string]int]{func(_ context.Context, key int) (map[string]int, bool, error) {
		reads[key]++
		return map[string]int{"a": 0}, key != 0, nil
	}}}, config.CacheConfig{ProductMapEntries: 2})
	for _, key := range []int{1, 2, 1, 3, 2, 0, 0} {
		_, _ = r.LoadProductMap(context.Background(), key)
	}
	if reads[1] != 1 || reads[2] != 2 || reads[0] != 2 || len(r.mappings.keys()) != 2 {
		t.Fatal(reads)
	}
}

func TestCacheMergedReadsIndependentCancellationAndDifferentKeys(t *testing.T) {
	entered := make(chan int, 3)
	release := make(chan struct{})
	var calls atomic.Int32
	r := testReader(t, Sources{Maps: []Source[int, map[string]int]{func(ctx context.Context, key int) (map[string]int, bool, error) {
		calls.Add(1)
		entered <- key
		select {
		case <-release:
			return map[string]int{"a": key}, true, nil
		case <-ctx.Done():
			return nil, false, ctx.Err()
		}
	}}}, config.CacheConfig{Concurrency: 2})
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := r.LoadProductMap(ctx, 1); first <- err }()
	<-entered
	second := make(chan error, 1)
	go func() { _, err := r.LoadProductMap(context.Background(), 1); second <- err }()
	different := make(chan error, 1)
	go func() { _, err := r.LoadProductMap(context.Background(), 2); different <- err }()
	select {
	case key := <-entered:
		if key != 2 {
			t.Fatal("same key not merged")
		}
	case <-time.After(time.Second):
		t.Fatal("different key blocked")
	}
	cancel()
	if err := <-first; err == nil {
		t.Fatal("cancellation ignored")
	}
	close(release)
	if err := <-second; err != nil {
		t.Fatal(err)
	}
	if err := <-different; err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatal(calls.Load())
	}
}

func TestReaderBackgroundRefreshAndClose(t *testing.T) {
	var version atomic.Int32
	version.Store(1)
	updated := make(chan struct{}, 1)
	r := testReader(t, Sources{Maps: []Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) {
		return map[string]int{"a": int(version.Load())}, true, nil
	}}}, config.CacheConfig{RefreshInterval: time.Millisecond})
	_, _ = r.LoadProductMap(context.Background(), 1)
	version.Store(2)
	r.Start(func(stats RefreshStats) {
		if stats.Checked > 0 {
			select {
			case updated <- struct{}{}:
			default:
			}
		}
	})
	r.Start(nil)
	select {
	case <-updated:
	case <-time.After(time.Second):
		t.Fatal("background refresh did not run")
	}
	value, _ := r.LoadProductMap(context.Background(), 1)
	if value["a"] != 2 {
		t.Fatal(value)
	}
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := r.LoadProductMap(context.Background(), 1); err == nil {
		t.Fatal("closed reader accepted load")
	}
	r.Start(nil)
}

func TestReaderCloseCancelsSourceAndBoundsConcurrency(t *testing.T) {
	var active, maxActive atomic.Int32
	entered := make(chan struct{}, 2)
	r := testReader(t, Sources{Maps: []Source[int, map[string]int]{func(ctx context.Context, _ int) (map[string]int, bool, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := maxActive.Load(); n > old; old = maxActive.Load() {
			if maxActive.CompareAndSwap(old, n) {
				break
			}
		}
		entered <- struct{}{}
		<-ctx.Done()
		return nil, false, ctx.Err()
	}}}, config.CacheConfig{Concurrency: 2})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, _ = r.LoadProductMap(context.Background(), i) }()
	}
	<-entered
	<-entered
	if err := r.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if maxActive.Load() > 2 || active.Load() != 0 {
		t.Fatal("unbounded or leaked reads")
	}
}

func TestNewRejectsInvalidConfig(t *testing.T) {
	if _, err := New(Sources{}, config.CacheConfig{}, time.Second); err == nil {
		t.Fatal("missing sources accepted")
	}
	if _, err := New(Sources{}, config.CacheConfig{Concurrency: -1}, time.Second); err == nil {
		t.Fatal("negative limit accepted")
	}
}

func TestCacheAdmissionWaitHasTimeout(t *testing.T) {
	r := testReader(t, Sources{}, config.CacheConfig{Concurrency: 1})
	r.mappings.timeout = 10 * time.Millisecond
	r.mappings.gate <- struct{}{}
	defer func() { <-r.mappings.gate }()
	_, err := r.LoadProductMap(context.Background(), 1)
	if !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, services.ErrUnavailable) {
		t.Fatalf("queue timeout=%v", err)
	}
	if len(r.mappings.keys()) != 0 {
		t.Fatal("failed admission cached value")
	}
}

func TestReaderDisabledReadsEverySourceAndNeverCaches(t *testing.T) {
	credential := auth.Credential{Version: 1, Secret: "old", Status: 1}
	access := auth.ProductAccess{Version: 1, Status: 1}
	mapping := map[string]int{"a": 0}
	var readErr error
	counts := [3]int{}
	r := testReader(t, Sources{
		Credentials: []Source[string, auth.Credential]{func(context.Context, string) (auth.Credential, bool, error) {
			counts[0]++
			return credential, true, readErr
		}},
		Products: []Source[ProductKey, auth.ProductAccess]{func(context.Context, ProductKey) (auth.ProductAccess, bool, error) {
			counts[1]++
			return access, true, readErr
		}},
		Maps: []Source[int, map[string]int]{func(context.Context, int) (map[string]int, bool, error) { counts[2]++; return mapping, true, readErr }},
	}, config.CacheConfig{Disabled: true})
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := r.LoadCredential(ctx, "key"); err != nil {
			t.Fatal(err)
		}
		if err := r.CheckProductAccess(ctx, "key", 10001); err != nil {
			t.Fatal(err)
		}
		value, err := r.LoadProductMap(ctx, 10001)
		if err != nil {
			t.Fatal(err)
		}
		value["a"] = 99
	}
	if counts != ([3]int{2, 2, 2}) || mapping["a"] != 0 {
		t.Fatal("direct read cached or shared mutable result", counts)
	}
	credential.Secret = "new"
	v, err := r.LoadCredential(ctx, "key")
	if err != nil || v.Secret != "new" {
		t.Fatal("rotation not immediate")
	}
	access.Status = 0
	if err := r.CheckProductAccess(ctx, "key", 10001); !errors.Is(err, auth.ErrProductDenied) {
		t.Fatal("revocation not immediate")
	}
	readErr = errors.New("network")
	if _, err := r.LoadCredential(ctx, "key"); !errors.Is(err, auth.ErrUnavailable) {
		t.Fatal("used stale credential")
	}
	if _, err := r.LoadProductMap(ctx, 10001); !errors.Is(err, services.ErrUnavailable) {
		t.Fatal("used stale mapping")
	}
	before := counts
	r.Start(func(RefreshStats) { t.Error("disabled reader started worker") })
	if stats := r.Refresh(ctx); stats.Checked != 0 || counts != before || r.started {
		t.Fatal("disabled reader refreshed")
	}
	if len(r.credentials.keys())+len(r.products.keys())+len(r.mappings.keys()) != 0 {
		t.Fatal("disabled reader populated memory")
	}
}

func TestDisabledReaderCloseCancelsDirectReads(t *testing.T) {
	entered := make(chan struct{})
	r := testReader(t, Sources{Maps: []Source[int, map[string]int]{func(ctx context.Context, _ int) (map[string]int, bool, error) {
		close(entered)
		<-ctx.Done()
		return nil, false, ctx.Err()
	}}}, config.CacheConfig{Disabled: true})
	finished := make(chan error, 1)
	go func() { _, err := r.LoadProductMap(context.Background(), 1); finished <- err }()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := r.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-finished; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := r.LoadProductMap(context.Background(), 1); err == nil {
		t.Fatal("closed direct reader accepted request")
	}
}
