package mongo

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/kalaGN/gin-finapi/pkg/config"
)

func TestDecodeUserData_PreservesAllVariableMappings(t *testing.T) {
	want := map[string]int{
		"var100001": 10, "var100002": -2, "var100003": 0,
		"var100004": 40, "var100005": 50, "var100006": 60,
	}
	got, err := decodeUserData(context.Background(), gzipBytes(t, []byte("10,-2,0,40,50,60")), testVariableMapping())
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestDecodeUserData_UsesSuppliedMappingWithoutFallback(t *testing.T) {
	data := gzipBytes(t, []byte("10,20,30,40,50,60"))
	got, err := decodeUserData(context.Background(), data, map[string]int{"selected": 2, "last": 5})
	if err != nil || !reflect.DeepEqual(got, map[string]int{"selected": 30, "last": 60}) {
		t.Fatalf("data=%v err=%v", got, err)
	}
	if _, err := decodeUserData(context.Background(), data, nil); !errors.Is(err, ErrInvalidData) {
		t.Fatal("missing mapping accepted")
	}
	store, err := NewStore(config.MongoDBConfig{DSN: "mongodb://localhost:27017", Database: "test", MaxPool: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(context.Background()); err != nil {
			t.Error(err)
		}
	})
	if _, err := store.Query(context.Background(), "test", nil); !errors.Is(err, ErrInvalidData) {
		t.Fatal("query accepted missing mapping")
	}
	if _, err := decodeUserData(context.Background(), data, map[string]int{"selected": 2}); !errors.Is(err, ErrInvalidData) {
		t.Fatal("strict CSV length changed")
	}
}

func TestDecodeUserData_PreservesStrictPayloadValidation(t *testing.T) {
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"empty binary", nil},
		{"invalid gzip", []byte("not gzip")},
		{"empty CSV", gzipBytes(t, nil)},
		{"missing field", gzipBytes(t, []byte("1,2,3,4,5"))},
		{"extra field", gzipBytes(t, []byte("1,2,3,4,5,6,7"))},
		{"noninteger", gzipBytes(t, []byte("1,2,3,4,5,x"))},
		{"empty field", gzipBytes(t, []byte("1,2,3,4,5,"))},
		{"decimal", gzipBytes(t, []byte("1,2,3,4,5,6.5"))},
		{"space padded", gzipBytes(t, []byte("1,2,3,4,5, 6"))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeUserData(context.Background(), tc.data, testVariableMapping())
			if !errors.Is(err, ErrInvalidData) || got != nil {
				t.Fatalf("got %#v, error %v; want nil data and ErrInvalidData", got, err)
			}
		})
	}
}
