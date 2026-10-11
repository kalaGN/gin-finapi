package bootstrap

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"testing"
)

func TestRunHTTP_ServiceFailureStillClosesDependencies(t *testing.T) {
	failure := errors.New("listen failed")
	var calls []string
	err := runHTTP(context.Background(), httpLifecycle{
		serve:    func() error { return failure },
		shutdown: func(context.Context) error { calls = append(calls, "http"); return nil },
	}, func(context.Context) error { calls = append(calls, "dependencies"); return nil })
	if !errors.Is(err, failure) || !reflect.DeepEqual(calls, []string{"http", "dependencies"}) {
		t.Fatalf("error=%v calls=%v", err, calls)
	}
}

func TestRunHTTP_CancellationWaitsForServerAndTreatsClosedAsNormal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	stopped := make(chan struct{})
	finished := make(chan struct{})
	err := runHTTP(ctx, httpLifecycle{
		serve: func() error { defer close(finished); <-stopped; return http.ErrServerClosed },
		shutdown: func(ctx context.Context) error {
			if ctx.Err() != nil {
				t.Fatal("shutdown inherited canceled signal")
			}
			close(stopped)
			return nil
		},
	}, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	default:
		t.Fatal("server goroutine not joined")
	}
}

func TestShutdownResources_IndependentContextsAndForcedClose(t *testing.T) {
	var httpCtx context.Context
	var calls []string
	forceFailure := errors.New("forced close failed")
	dependencyFailure := errors.New("dependency close failed")
	err := shutdownResources(func(ctx context.Context) error {
		httpCtx = ctx
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("HTTP missing deadline")
		}
		calls = append(calls, "http")
		return context.DeadlineExceeded
	}, func() error { calls = append(calls, "force"); return forceFailure }, func(ctx context.Context) error {
		if ctx == httpCtx || ctx.Err() != nil {
			t.Fatal("dependency context not independent")
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Fatal("dependency missing deadline")
		}
		calls = append(calls, "dependencies")
		return dependencyFailure
	})
	if !reflect.DeepEqual(calls, []string{"http", "force", "dependencies"}) || !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, forceFailure) || !errors.Is(err, dependencyFailure) {
		t.Fatalf("calls=%v error=%v", calls, err)
	}
}
