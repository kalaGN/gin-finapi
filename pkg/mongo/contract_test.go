package mongo

import (
	"bytes"
	"testing"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/bsontype"
)

func TestLookupFilter_PreservesBSONQuery(t *testing.T) {
	data, err := bson.Marshal(lookupFilter{T: "test-query-key"})
	if err != nil {
		t.Fatal(err)
	}
	raw := bson.Raw(data)
	elements, err := raw.Elements()
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 1 || elements[0].Key() != "t" || raw.Lookup("t").Type != bsontype.String {
		t.Fatalf("query format changed: %v", raw)
	}
	if got := raw.Lookup("t").StringValue(); got != "test-query-key" {
		t.Fatalf("query key = %q", got)
	}
}

func TestStoredDocument_PreservesBSONAndGzipCSV(t *testing.T) {
	payload := gzipBytes(t, []byte("1,2,3,4,5,6"))
	data, err := bson.Marshal(storedDocument{T: "test-document-key", V: payload})
	if err != nil {
		t.Fatal(err)
	}
	raw := bson.Raw(data)
	elements, err := raw.Elements()
	if err != nil {
		t.Fatal(err)
	}
	if len(elements) != 2 || raw.Lookup("t").Type != bsontype.String || raw.Lookup("v").Type != bsontype.Binary {
		t.Fatal("document must contain only string t and binary v")
	}
	subtype, binary := raw.Lookup("v").Binary()
	if subtype != 0 || !bytes.Equal(binary, payload) {
		t.Fatal("binary payload encoding changed")
	}
	var document storedDocument
	if err := bson.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	if document.T != "test-document-key" || !bytes.Equal(document.V, payload) {
		t.Fatal("BSON decoding changed")
	}
	got, err := decodeUserData(document.V, testVariableMapping())
	if err != nil {
		t.Fatal(err)
	}
	for i, key := range []string{"var100001", "var100002", "var100003", "var100004", "var100005", "var100006"} {
		if got[key] != i+1 {
			t.Fatalf("%s = %d, want %d", key, got[key], i+1)
		}
	}
}
