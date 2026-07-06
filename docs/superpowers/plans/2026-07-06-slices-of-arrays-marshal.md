# Slices of Arrays — Marshal Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the `do not yet support slices of array yet` panic by adding `case reflect.Array` to the marshal generators for slices and arrays.

**Architecture:** Add a `case reflect.Array` branch to two existing type switches in `gen.go` (`emitCborMarshalSliceField` and `emitCborMarshalArrayField`), dispatching to the already-existing `emitCborMarshalArrayField`. Mirror the unmarshal side. Add a test type exercising `[][4]byte`, `[][3]uint64`, and nested `[][2][3]byte`; regenerate and round-trip.

**Tech Stack:** Go (go.mod), cbor-gen code generator, `testing/quick` roundtrip tests.

## Global Constraints

- CBOR output must conform to RFC 8949 (major type 4 arrays nest naturally; `[N]byte` encodes as major type 2 byte string — already handled by existing array marshaler).
- Marshal support must be symmetric with the existing unmarshal support (`gen.go:1501`, `gen.go:1653` already handle `reflect.Array` inside slices/arrays).
- No new element kinds beyond what marshal already supports (Struct, Uint64, Uint8, Int64, Slice, String, Array).
- Regenerate test fixtures via `make gentest`; verify via `make test`.

---

### Task 1: Add `case reflect.Array` to `emitCborMarshalSliceField`

**Files:**
- Modify: `gen.go:768-783` (the `switch e.Kind()` in `emitCborMarshalSliceField`)

**Interfaces:**
- Consumes: `emitCborMarshalArrayField(w, subf)` (already defined at `gen.go:801`), `subf` is `{Name: "v", Type: e, Pkg: f.Pkg, Pointer: pointer}`.
- Produces: marshal support for `[][N]T` fields where `T` is any already-supported array element kind.

- [ ] **Step 1: Add the case to the slice marshal switch**

In `gen.go`, inside `emitCborMarshalSliceField`, locate the switch at line 768:

```go
	subf := Field{Name: "v", Type: e, Pkg: f.Pkg, Pointer: pointer}
	switch e.Kind() {
	default:
		err = fmt.Errorf("do not yet support slices of %s yet", e.Kind())
	case reflect.Struct:
		err = g.emitCborMarshalStructField(w, subf)
	case reflect.Uint64:
		err = g.emitCborMarshalUint64Field(w, subf)
	case reflect.Uint8:
		err = g.emitCborMarshalUint8Field(w, subf)
	case reflect.Int64:
		err = g.emitCborMarshalInt64Field(w, subf)
	case reflect.Slice:
		err = g.emitCborMarshalSliceField(w, subf)
	case reflect.String:
		err = g.emitCborMarshalStringField(w, subf)
	}
```

Add `case reflect.Array` dispatching to `emitCborMarshalArrayField`:

```go
	subf := Field{Name: "v", Type: e, Pkg: f.Pkg, Pointer: pointer}
	switch e.Kind() {
	default:
		err = fmt.Errorf("do not yet support slices of %s yet", e.Kind())
	case reflect.Struct:
		err = g.emitCborMarshalStructField(w, subf)
	case reflect.Uint64:
		err = g.emitCborMarshalUint64Field(w, subf)
	case reflect.Uint8:
		err = g.emitCborMarshalUint8Field(w, subf)
	case reflect.Int64:
		err = g.emitCborMarshalInt64Field(w, subf)
	case reflect.Slice:
		err = g.emitCborMarshalSliceField(w, subf)
	case reflect.Array:
		err = g.emitCborMarshalArrayField(w, subf)
	case reflect.String:
		err = g.emitCborMarshalStringField(w, subf)
	}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add gen.go
git commit -m "feat: support slices of arrays in marshal generator"
```

---

### Task 2: Add `case reflect.Array` to `emitCborMarshalArrayField`

**Files:**
- Modify: `gen.go:840-855` (the `switch e.Kind()` in `emitCborMarshalArrayField`)

**Interfaces:**
- Consumes: `emitCborMarshalArrayField(w, subf)` (recursive, already defined at `gen.go:801`), `subf` is `{Name: "v", Type: e, Pkg: f.Pkg, Pointer: pointer}`.
- Produces: marshal support for `[M][N]T` nested fixed-array fields.

- [ ] **Step 1: Add the case to the array marshal switch**

In `gen.go`, inside `emitCborMarshalArrayField`, locate the switch at line 840:

```go
	subf := Field{Name: "v", Type: e, Pkg: f.Pkg, Pointer: pointer}
	switch e.Kind() {
	default:
		err = fmt.Errorf("do not yet support arrays of %s yet", e.Kind())
	case reflect.Struct:
		err = g.emitCborMarshalStructField(w, subf)
	case reflect.Uint64:
		err = g.emitCborMarshalUint64Field(w, subf)
	case reflect.Uint8:
		err = g.emitCborMarshalUint8Field(w, subf)
	case reflect.Int64:
		err = g.emitCborMarshalInt64Field(w, subf)
	case reflect.Slice:
		err = g.emitCborMarshalSliceField(w, subf)
	case reflect.String:
		err = g.emitCborMarshalStringField(w, subf)
	}
```

Add `case reflect.Array`:

```go
	subf := Field{Name: "v", Type: e, Pkg: f.Pkg, Pointer: pointer}
	switch e.Kind() {
	default:
		err = fmt.Errorf("do not yet support arrays of %s yet", e.Kind())
	case reflect.Struct:
		err = g.emitCborMarshalStructField(w, subf)
	case reflect.Uint64:
		err = g.emitCborMarshalUint64Field(w, subf)
	case reflect.Uint8:
		err = g.emitCborMarshalUint8Field(w, subf)
	case reflect.Int64:
		err = g.emitCborMarshalInt64Field(w, subf)
	case reflect.Slice:
		err = g.emitCborMarshalSliceField(w, subf)
	case reflect.Array:
		err = g.emitCborMarshalArrayField(w, subf)
	case reflect.String:
		err = g.emitCborMarshalStringField(w, subf)
	}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add gen.go
git commit -m "feat: support arrays of arrays in marshal generator"
```

---

### Task 3: Add `SlicesOfArrays` test type

**Files:**
- Modify: `testing/types.go` (append a new type at end of file)

**Interfaces:**
- Produces: `types.SlicesOfArrays` struct used by Task 4 (registration) and Task 5 (roundtrip test).

- [ ] **Step 1: Add the type**

Append to `testing/types.go` (after `BinaryMarshalerTuple`):

```go
type SlicesOfArrays struct {
	ByteArrs [][4]byte
	IntArrs  [][3]uint64
	Nested   [][2][3]byte
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./testing`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add testing/types.go
git commit -m "test: add SlicesOfArrays fixture type"
```

---

### Task 4: Register `SlicesOfArrays` with the generator

**Files:**
- Modify: `testgen/main.go` (the `WriteTupleEncodersToFile` call starting at line 9)

**Interfaces:**
- Consumes: `types.SlicesOfArrays{}` from Task 3.
- Produces: regenerated `testing/cbor_gen.go` containing marshal/unmarshal methods for `SlicesOfArrays`.

- [ ] **Step 1: Add the type to the generator invocation**

In `testgen/main.go`, add `types.SlicesOfArrays{},` to the `WriteTupleEncodersToFile` argument list. Insert it after `types.BinaryMarshalerTuple{},` (line 26) so the resulting block reads:

```go
	if err := cbg.WriteTupleEncodersToFile("testing/cbor_gen.go", "testing",
		types.SignedArray{},
		types.SimpleTypeOne{},
		types.SimpleTypeTwo{},
		types.DeferredContainer{},
		types.FixedArrays{},
		types.ThingWithSomeTime{},
		types.BigField{},
		types.IntArray{},
		types.IntAliasArray{},
		types.TupleIntArray{},
		types.TupleIntArrayOptionals{},
		types.IntArrayNewType{},
		types.IntArrayAliasNewType{},
		types.MapTransparentType{},
		types.BigIntContainer{},
		types.TupleWithOptionalFields{},
		types.BinaryMarshalerTuple{},
		types.SlicesOfArrays{},
	); err != nil {
		panic(err)
	}
```

- [ ] **Step 2: Regenerate the test fixtures**

Run: `make gentest`
Expected: command exits 0, `testing/cbor_gen.go` is updated with `SlicesOfArrays` marshal/unmarshal methods.

- [ ] **Step 3: Verify the generated code compiles**

Run: `go build ./testing`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add testgen/main.go testing/cbor_gen.go
git commit -m "test: regenerate fixtures with SlicesOfArrays"
```

---

### Task 5: Add roundtrip test

**Files:**
- Modify: `testing/roundtrip_test.go` (append after `TestSimpleTypeTwo`, near line 44)

**Interfaces:**
- Consumes: `types.SlicesOfArrays` (Task 3), `testTypeRoundtrips` helper (existing, `roundtrip_test.go:149`).

- [ ] **Step 1: Add the test function**

Insert after `TestSimpleTypeTwo` (line 44):

```go
func TestSlicesOfArrays(t *testing.T) {
	testTypeRoundtrips(t, reflect.TypeOf(SlicesOfArrays{}))
}
```

- [ ] **Step 2: Run the new test**

Run: `go test ./testing -run TestSlicesOfArrays -v`
Expected: PASS (1000 quickcheck iterations round-trip without errors).

- [ ] **Step 3: Run the full test suite**

Run: `make test`
Expected: all tests pass.

- [ ] **Step 4: Commit**

```bash
git add testing/roundtrip_test.go
git commit -m "test: roundtrip SlicesOfArrays"
```
