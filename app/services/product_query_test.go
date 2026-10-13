package services

import (
	"context"
	"errors"
	"testing"
)

func TestQuery_LoadsMappingBeforeDataAndPreservesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	loadCalls := 0
	query, err := NewQuery(func(actual context.Context, pcode int) (map[string]int, error) {
		loadCalls++
		if actual != ctx {
			t.Fatal("context changed")
		}
		return map[string]int{"selected": pcode}, nil
	}, func(actual context.Context, phone string, mapping map[string]int) (map[string]int, error) {
		if actual != ctx || phone != "lookup" {
			t.Fatal("query changed")
		}
		return mapping, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, pcode := range []int{10001, 10002} {
		got, err := query(ctx, "lookup", pcode)
		if err != nil || got["selected"] != pcode {
			t.Fatalf("got=%v err=%v", got, err)
		}
	}
	if loadCalls != 2 {
		t.Fatal("mapping was cached")
	}
}

func TestQuery_MappingFailureDoesNotQueryData(t *testing.T) {
	failure := errors.New("mapping failed")
	query, err := NewQuery(func(context.Context, int) (map[string]int, error) { return nil, failure }, func(context.Context, string, map[string]int) (map[string]int, error) {
		t.Fatal("unexpected data query")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := query(context.Background(), "lookup", 10001); !errors.Is(err, failure) {
		t.Fatalf("err=%v", err)
	}
	if _, err := NewQuery(nil, nil); err == nil {
		t.Fatal("missing dependencies accepted")
	}
}
