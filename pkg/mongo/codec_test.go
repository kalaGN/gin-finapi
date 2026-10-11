package mongo

import (
	"errors"
	"reflect"
	"testing"
)

func TestDecodeUserData_PreservesAllVariableMappings(t *testing.T) {
	want := map[string]int{
		"var100001": 10, "var100002": -2, "var100003": 0,
		"var100004": 40, "var100005": 50, "var100006": 60,
	}
	got, err := decodeUserData(gzipBytes(t, []byte("10,-2,0,40,50,60")))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
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
			got, err := decodeUserData(tc.data)
			if !errors.Is(err, ErrInvalidData) || got != nil {
				t.Fatalf("got %#v, error %v; want nil data and ErrInvalidData", got, err)
			}
		})
	}
}
