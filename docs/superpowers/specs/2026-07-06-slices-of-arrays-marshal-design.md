# Slices of Arrays — Marshal Support

## Problem

`cbor-gen` panics with `do not yet support slices of array yet` when generating
encoders for a struct containing a field of type `[]T` where `T` is itself a
fixed-size array (e.g. `[][4]byte`, `[][3]uint64`).

The unmarshal generators already support arrays-in-slices and arrays-in-arrays
(`gen.go:1501`, `gen.go:1653`). The marshal generators do not — the type
switches in `emitCborMarshalSliceField` (`gen.go:768`) and
`emitCborMarshalArrayField` (`gen.go:840`) are missing `case reflect.Array`.

## Goal

Add `case reflect.Array` to both marshal switches so that marshal and unmarshal
support arrays as element kinds symmetrically. The change must produce
CBOR-conformant output per RFC 8949.

## CBOR Conformance

CBOR (RFC 8949) does not distinguish between Go slices and Go arrays at the
encoding level — both become major type 4 (array). Nesting is well-defined:

- `[][4]byte` — CBOR array whose elements are byte strings (major type 2),
  reusing the existing `[N]byte` array encoder.
- `[][3]uint64` — CBOR array whose elements are CBOR arrays of unsigned
  integers.
- `[][2][3]byte` — nested arrays of byte strings.

All encodings round-trip against the existing unmarshal generators, which
already handle `reflect.Array` inside slices/arrays.

## Approach

In `gen.go`, add the following case to both `emitCborMarshalSliceField` and
`emitCborMarshalArrayField`:

```go
case reflect.Array:
    err = g.emitCborMarshalArrayField(w, subf)
```

where `subf` is the already-constructed `Field{Name: "v", Type: e, Pkg: f.Pkg,
Pointer: pointer}`. This recurses into the existing array marshaler, mirroring
the unmarshal side.

### Variable shadowing

The marshal templates use a hardcoded loop variable `v`:

```go
for _, v := range {{ .Name }} {
```

When arrays nest (e.g. `[][2][3]byte`), the inner array template emits another
`for _, v := range v`, which is legal Go — the inner `v` shadows the outer.
This is the same pattern already used for `[][]int64` (handled via
`case reflect.Slice` at `gen.go:779`/`gen.go:851`). No `IterLabel` plumbing is
needed on the marshal side.

## Testing

1. Add a new type to `testing/types.go`:

   ```go
   type SlicesOfArrays struct {
       ByteArrs [][4]byte
       IntArrs  [][3]uint64
       Nested   [][2][3]byte
   }
   ```

2. Register it in `testgen/main.go` via `WriteTupleEncodersToFile`.

3. Add `TestSlicesOfArrays` to `testing/roundtrip_test.go` using the existing
   `testTypeRoundtrips` helper, which runs 1000 quickcheck round-trips.

4. Regenerate `testing/cbor_gen.go` via `make gentest` and run `make test`.

## Files Changed

- `gen.go` — two `case reflect.Array` additions.
- `testing/types.go` — new `SlicesOfArrays` type.
- `testgen/main.go` — register the new type.
- `testing/roundtrip_test.go` — new test.
- `testing/cbor_gen.go` — regenerated.

## Out of Scope

- No unmarshal changes (already supported).
- No `IterLabel` / template plumbing changes.
- No new element kinds beyond what marshal already supports
  (Struct, Uint64, Uint8, Int64, Slice, String, now Array).
