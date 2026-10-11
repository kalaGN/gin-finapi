package bootstrap

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestShutdownResources_ClosesHTTPBeforeMongo(t *testing.T) {
	var calls []string
	err := shutdownResources(
		func(context.Context) error {
			calls = append(calls, "http")
			return nil
		},
		nil,
		func(context.Context) error {
			calls = append(calls, "mongo")
			return nil
		},
	)
	if err != nil {
		t.Fatalf("shutdownResources() error = %v", err)
	}
	if want := []string{"http", "mongo"}; !reflect.DeepEqual(calls, want) {
		t.Fatalf("shutdown order = %v, want %v", calls, want)
	}
}

func TestShutdownResources_AttemptsBothAndJoinsErrors(t *testing.T) {
	httpErr := errors.New("http shutdown failed")
	mongoErr := errors.New("mongo close failed")
	mongoCalled := false

	err := shutdownResources(
		func(context.Context) error { return httpErr },
		nil,
		func(context.Context) error {
			mongoCalled = true
			return mongoErr
		},
	)
	if !mongoCalled {
		t.Fatal("Mongo close was skipped after HTTP shutdown failure")
	}
	if !errors.Is(err, httpErr) || !errors.Is(err, mongoErr) {
		t.Fatalf("shutdownResources() error = %v, want both errors", err)
	}
}

func TestShutdownResources_ClosesRedisAfterHTTPAndMongoFailures(t *testing.T) {
	want := errors.New("Redis close failed")
	var calls []string
	err := shutdownResources(func(context.Context) error { calls = append(calls, "http"); return errors.New("HTTP failed") },
		nil,
		func(context.Context) error {
			calls = append(calls, "mongo")
			mongoErr := errors.New("Mongo failed")
			calls = append(calls, "redis")
			return errors.Join(mongoErr, want)
		},
	)
	if !reflect.DeepEqual(calls, []string{"http", "mongo", "redis"}) || !errors.Is(err, want) {
		t.Fatalf("calls=%v error=%v", calls, err)
	}
}
