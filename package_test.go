package typegen

import (
	"reflect"
	"testing"
)

type namedArray [16]byte

// typeName renders named composite types (e.g. ulid.ULID) by name, so their
// package must be imported even though the element type is builtin.
func TestImportsForNamedCompositeType(t *testing.T) {
	want := []Import{{Name: "cbg", PkgPath: "github.com/whyrusleeping/cbor-gen"}}
	for _, v := range []any{namedArray{}, []namedArray{}, &namedArray{}, map[string]namedArray{}} {
		typ := reflect.TypeOf(v)
		if got := ImportsForType("example.com/other", typ); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %v, want %v", typ, got, want)
		}
		if got := ImportsForType("github.com/whyrusleeping/cbor-gen", typ); len(got) != 0 {
			t.Errorf("%s in its own package: got %v, want none", typ, got)
		}
	}
}
