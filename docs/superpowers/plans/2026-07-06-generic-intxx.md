# Generic IntXX Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the six width-specific integer marshal/unmarshal functions with two generic functions covering `int8/16/32/64` and `uint8/16/32/64`, routing all eight kinds via every type switch.

**Architecture:** Add `emitCborMarshalIntField` and `emitCborUnmarshalIntField` parameterized by signedness and bit-width (computed at gen time from `f.Type.Kind()` and `f.Type.Bits()`). Update eight type switches to route all eight `reflect.Int*`/`reflect.Uint*` kinds to them. Delete the six old per-width functions. Add a fixture type and roundtrip + overflow tests.

**Tech Stack:** Go (go.mod), cbor-gen code generator, `text/template`, `testing/quick`.

## Global Constraints

- CBOR output must conform to RFC 8949 §3.1: major type 0 (unsigned int) for non-negative values, major type 1 (negative int) for negative signed values. The "additional information" bits in the header carry the value/length and are handled by the existing `MajorType` template helper and `cbg.ReadHeader` — no changes to those helpers.
- **Every unmarshal path MUST return an error when the incoming CBOR value does not fit in the target Go field's width.** This is the central correctness property.
- Bit width is computed at generation time from `f.Type.Bits()`; overflow thresholds are emitted as **literal integer constants** in the generated code (no runtime reflection).
- Exclude platform-dependent `int`, `uint`, `uintptr` (non-portable generated code).
- Keep the `[]byte` / `[N]byte` fast paths special-cased to `MajByteString`.
- Regenerate test fixtures via `make gentest`; verify via `make test`.

---

### Task 1: Add `emitCborMarshalIntField`

**Files:**
- Modify: `gen.go` (insert new function near line 576, before the old `emitCborMarshalUint64Field`)

**Interfaces:**
- Consumes: `g.doTemplate(w, info, templ)` (`gen.go:61`), `MajorType` template helper, `Field` struct (`gen.go:141`).
- Produces: `func (g Gen) emitCborMarshalIntField(w io.Writer, f Field) error` — marshals any of `int8/16/32/64`, `uint8/16/32/64` (pointer or value) to CBOR major type 0 or 1.

- [ ] **Step 1: Add the generic marshal function**

Insert this function in `gen.go` immediately before `emitCborMarshalUint64Field` (line 576):

```go
func (g Gen) emitCborMarshalIntField(w io.Writer, f Field) error {
	signed := f.Type.Kind() >= reflect.Int8 && f.Type.Kind() <= reflect.Int64
	data := struct {
		Field
		Signed bool
	}{Field: f, Signed: signed}

	return g.doTemplate(w, data, `{{ if .Pointer }}
	if {{ .Name }} == nil {
		if _, err := cw.Write(cbg.CborNull); err != nil {
			return err
		}
	} else {
		{{ if .Signed }}
		if {{ (print "*" .Name) }} >= 0 {
		{{ MajorType "cw" "cbg.MajUnsignedInt" (print "*" .Name) }}
		} else {
		{{ MajorType "cw" "cbg.MajNegativeInt" (print "-*" .Name "-1") }}
		}
		{{ else }}
		{{ MajorType "cw" "cbg.MajUnsignedInt" (print "*" .Name) }}
		{{ end }}
	}
{{ else }}
	{{ if .Signed }}
	if {{ .Name }} >= 0 {
	{{ MajorType "cw" "cbg.MajUnsignedInt" .Name }}
	} else {
	{{ MajorType "cw" "cbg.MajNegativeInt" (print "-" .Name "-1") }}
	}
	{{ else }}
	{{ MajorType "cw" "cbg.MajUnsignedInt" .Name }}
	{{ end }}
{{ end }}
`)
}
```

**Notes for the implementer:**
- `reflect.Int8` through `reflect.Int64` are contiguous enum values; the `>= reflect.Int8 && <= reflect.Int64` check distinguishes signed from unsigned at generation time.
- The template embeds `Field` (so `.Name`, `.Pointer`, `.TypeName` still work) and adds `.Signed`.
- This template is the union of the existing `Uint64`/`Int64` templates; the `{{ if .Signed }}` branches select between them at generation time.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors (the new function is unused but compile-clean).

- [ ] **Step 3: Commit**

```bash
git add gen.go
git commit -m "feat(gen): add generic emitCborMarshalIntField"
```

---

### Task 2: Add `emitCborUnmarshalIntField`

**Files:**
- Modify: `gen.go` (insert new function near line 1133, before the old `emitCborUnmarshalInt64Field`)

**Interfaces:**
- Consumes: `g.doTemplate`, `ReadHeader` template helper, `Field` struct, `f.Type.Bits()`.
- Produces: `func (g Gen) emitCborUnmarshalIntField(w io.Writer, f Field) error` — unmarshals any of `int8/16/32/64`, `uint8/16/32/64` (pointer or value), returning an error on overflow.

- [ ] **Step 1: Add the generic unmarshal function**

Insert this function in `gen.go` immediately before `emitCborUnmarshalInt64Field` (line 1133). The overflow constants are computed at generation time and emitted as literals.

```go
func (g Gen) emitCborUnmarshalIntField(w io.Writer, f Field) error {
	bits := f.Type.Bits()
	signed := f.Type.Kind() >= reflect.Int8 && f.Type.Kind() <= reflect.Int64

	// Compute overflow thresholds as literal constants.
	maxUnsigned := uint64(1)<<uint(bits) - 1            // uint8: 255, uint16: 65535, ...
	maxPositive := uint64(1)<<uint(bits-1) - 1          // int8: 127, int16: 32767, ...
	maxNegative := uint64(1)<<uint(bits-1) - 1          // int8: 127, encoded as -1-127 = -128

	data := struct {
		Field
		Signed       bool
		MaxUnsigned  uint64
		MaxPositive  uint64
		MaxNegative  uint64
	}{Field: f, Signed: signed, MaxUnsigned: maxUnsigned,
		MaxPositive: maxPositive, MaxNegative: maxNegative}

	return g.doTemplate(w, data, `{
	{{ if .Pointer }}
	b, err := cr.ReadByte()
	if err != nil {
		return err
	}
	if b != cbg.CborNull[0] {
		if err := cr.UnreadByte(); err != nil {
			return err
		}
	{{ end }}
		maj, extra, err := {{ ReadHeader "cr" }}
		if err != nil {
			return err
		}
		{{ if .Signed }}
		var extraI int64
		switch maj {
		case cbg.MajUnsignedInt:
			if extra > {{ .MaxPositive }} {
				return fmt.Errorf("{{ .Name }}: value %d too large for {{ .TypeName }}", extra)
			}
			extraI = int64(extra)
		case cbg.MajNegativeInt:
			if extra > {{ .MaxNegative }} {
				return fmt.Errorf("{{ .Name }}: value %d too negative for {{ .TypeName }}", extra)
			}
			extraI = -1 - int64(extra)
		default:
			return fmt.Errorf("wrong type for {{ .TypeName }} field: %d", maj)
		}
		{{ else }}
		if maj != cbg.MajUnsignedInt {
			return fmt.Errorf("wrong type for {{ .TypeName }} field")
		}
		if extra > {{ .MaxUnsigned }} {
			return fmt.Errorf("{{ .Name }}: value %d too large for {{ .TypeName }}", extra)
		}
		{{ end }}
	{{ if .Pointer }}
		{{ if .Signed }}
		typed := {{ .TypeName }}(extraI)
		{{ .Name }} = &typed
		{{ else }}
		typed := {{ .TypeName }}(extra)
		{{ .Name }} = &typed
		{{ end }}
	}
	{{ else }}
		{{ if .Signed }}
		{{ .Name }} = {{ .TypeName }}(extraI)
		{{ else }}
		{{ .Name }} = {{ .TypeName }}(extra)
		{{ end }}
	{{ end }}
}
`)
}
```

**Notes for the implementer:**
- `f.Type.Bits()` returns the width in bits (8, 16, 32, 64) — this is a `reflect.Type` method available at generation time, not at runtime of the generated code.
- For `uint64` and `int64`, `bits == 64`, so `maxUnsigned == math.MaxUint64`, `maxPositive == math.MaxInt64`, `maxNegative == math.MaxInt64`. The check `extra > math.MaxInt64` is equivalent to the existing `int64(extra) < 0` sentinel — the new generated code keeps the same correctness property with a clearer literal comparison.
- The `{{ .MaxPositive }}` etc. render as literal integers (e.g. `127`, `32767`) in the generated code.

- [ ] **Step 2: Verify it compiles**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add gen.go
git commit -m "feat(gen): add generic emitCborUnmarshalIntField with overflow checks"
```

---

### Task 3: Route all int/uint kinds in tuple struct switches

**Files:**
- Modify: `gen.go:911-931` (`emitCborMarshalStructTuple` body switch)
- Modify: `gen.go:~1780-1794` (`emitCborUnmarshalStructTuple` body switch — find via the `reflect.Uint64`/`reflect.Uint8`/`reflect.Int64` cases around line 1783)

**Interfaces:**
- Consumes: `emitCborMarshalIntField` (Task 1), `emitCborUnmarshalIntField` (Task 2).

- [ ] **Step 1: Update the tuple marshal switch**

In `gen.go`, inside `emitCborMarshalStructTuple`, replace the three int cases at lines 920-931:

```go
		case reflect.Uint64:
			if err := g.emitCborMarshalUint64Field(w, f); err != nil {
				return err
			}
		case reflect.Uint8:
			if err := g.emitCborMarshalUint8Field(w, f); err != nil {
				return err
			}
		case reflect.Int64:
			if err := g.emitCborMarshalInt64Field(w, f); err != nil {
				return err
			}
```

with:

```go
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if err := g.emitCborMarshalIntField(w, f); err != nil {
				return err
			}
```

- [ ] **Step 2: Update the tuple unmarshal switch**

In `emitCborUnmarshalStructTuple`, find the matching three cases (around lines 1783-1794):

```go
		case reflect.Uint64:
			if err := g.emitCborUnmarshalUint64Field(w, f); err != nil {
				return err
			}
		case reflect.Uint8:
			if err := g.emitCborUnmarshalUint8Field(w, f); err != nil {
				return err
			}
		case reflect.Int64:
			if err := g.emitCborUnmarshalInt64Field(w, f); err != nil {
				return err
			}
```

replace with:

```go
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if err := g.emitCborUnmarshalIntField(w, f); err != nil {
				return err
			}
```

- [ ] **Step 3: Verify it compiles** (old functions still referenced elsewhere — do not delete yet)

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add gen.go
git commit -m "feat(gen): route tuple int/uint kinds to generic Int field"
```

---

### Task 4: Route all int/uint kinds in map struct switches

**Files:**
- Modify: `gen.go:1963-1974` (`emitCborMarshalStructMap` body switch)
- Modify: `gen.go:2089-2100` (`emitCborUnmarshalStructMap` body switch)

**Interfaces:**
- Consumes: `emitCborMarshalIntField` (Task 1), `emitCborUnmarshalIntField` (Task 2).

- [ ] **Step 1: Update the map marshal switch**

In `emitCborMarshalStructMap`, replace the three cases at lines 1963-1974:

```go
		case reflect.Uint64:
			if err := g.emitCborMarshalUint64Field(w, f); err != nil {
				return err
			}
		case reflect.Int64:
			if err := g.emitCborMarshalInt64Field(w, f); err != nil {
				return err
			}
		case reflect.Uint8:
			if err := g.emitCborMarshalUint8Field(w, f); err != nil {
				return err
			}
```

with:

```go
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if err := g.emitCborMarshalIntField(w, f); err != nil {
				return err
			}
```

- [ ] **Step 2: Update the map unmarshal switch**

In `emitCborUnmarshalStructMap`, replace the three cases at lines 2089-2100:

```go
		case reflect.Uint64:
			if err := g.emitCborUnmarshalUint64Field(w, f); err != nil {
				return err
			}
		case reflect.Int64:
			if err := g.emitCborUnmarshalInt64Field(w, f); err != nil {
				return err
			}
		case reflect.Uint8:
			if err := g.emitCborUnmarshalUint8Field(w, f); err != nil {
				return err
			}
```

with:

```go
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
			reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			if err := g.emitCborUnmarshalIntField(w, f); err != nil {
				return err
			}
```

- [ ] **Step 3: Verify it compiles**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 4: Commit**

```bash
git add gen.go
git commit -m "feat(gen): route map int/uint kinds to generic Int field"
```

---

### Task 5: Route all int/uint kinds in slice/array subfield switches

**Files:**
- Modify: `gen.go:768-783` (`emitCborMarshalSliceField` subfield switch)
- Modify: `gen.go:840-857` (`emitCborMarshalArrayField` subfield switch)
- Modify: `gen.go:1469-1538` (`emitCborUnmarshalSliceField` subfield switch)
- Modify: `gen.go:1621-1689` (`emitCborUnmarshalArrayField` subfield switch)

**Interfaces:**
- Consumes: `emitCborMarshalIntField` (Task 1), `emitCborUnmarshalIntField` (Task 2).

- [ ] **Step 1: Update slice marshal subfield switch**

In `emitCborMarshalSliceField` (the `switch e.Kind()` at line 768), replace:

```go
	case reflect.Uint64:
		err = g.emitCborMarshalUint64Field(w, subf)
	case reflect.Uint8:
		err = g.emitCborMarshalUint8Field(w, subf)
	case reflect.Int64:
		err = g.emitCborMarshalInt64Field(w, subf)
```

with:

```go
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		err = g.emitCborMarshalIntField(w, subf)
```

Note: the existing `case reflect.Uint8` in this switch is dead code (the `[]byte` fast path at `gen.go:717` returns earlier), but it currently exists. Removing it here is correct.

- [ ] **Step 2: Update array marshal subfield switch**

In `emitCborMarshalArrayField` (the `switch e.Kind()` at line 840), replace:

```go
	case reflect.Uint64:
		err = g.emitCborMarshalUint64Field(w, subf)
	case reflect.Uint8:
		err = g.emitCborMarshalUint8Field(w, subf)
	case reflect.Int64:
		err = g.emitCborMarshalInt64Field(w, subf)
```

with:

```go
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		err = g.emitCborMarshalIntField(w, subf)
```

- [ ] **Step 3: Update slice unmarshal subfield switch**

In `emitCborUnmarshalSliceField` (the `switch e.Kind()` at line 1469), replace the three cases at lines 1481-1490:

```go
	case reflect.Uint64:
		subf := Field{
			Type: e,
			Pkg:  f.Pkg,
			Name: f.Name + "[" + f.IterLabel + "]",
		}
		err := g.emitCborUnmarshalUint64Field(w, subf)
		if err != nil {
			return err
		}
	case reflect.Int64:
		subf := Field{
			Type: e,
			Pkg:  f.Pkg,
			Name: f.Name + "[" + f.IterLabel + "]",
		}
		err := g.emitCborUnmarshalInt64Field(w, subf)
		if err != nil {
			return err
		}
```

with a single case:

```go
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		subf := Field{
			Type: e,
			Pkg:  f.Pkg,
			Name: f.Name + "[" + f.IterLabel + "]",
		}
		if err := g.emitCborUnmarshalIntField(w, subf); err != nil {
			return err
		}
```

Note: the existing switch has no separate `case reflect.Uint8` for unmarshal (uint8 slices fall through to the byte fast path). The byte fast path at `gen.go:1415` returns earlier and is unchanged.

- [ ] **Step 4: Update array unmarshal subfield switch**

In `emitCborUnmarshalArrayField` (the `switch e.Kind()` at line 1621), replace the three cases at lines 1633-1652:

```go
	case reflect.Uint64:
		subf := Field{
			Type: e,
			Pkg:  f.Pkg,
			Name: f.Name + "[" + f.IterLabel + "]",
		}
		err := g.emitCborUnmarshalUint64Field(w, subf)
		if err != nil {
			return err
		}
	case reflect.Int64:
		subf := Field{
			Type: e,
			Pkg:  f.Pkg,
			Name: f.Name + "[" + f.IterLabel + "]",
		}
		err := g.emitCborUnmarshalInt64Field(w, subf)
		if err != nil {
			return err
		}
```

with a single case:

```go
	case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		subf := Field{
			Type: e,
			Pkg:  f.Pkg,
			Name: f.Name + "[" + f.IterLabel + "]",
		}
		if err := g.emitCborUnmarshalIntField(w, subf); err != nil {
			return err
		}
```

- [ ] **Step 5: Verify it compiles**

Run: `go build ./...`
Expected: no errors.

- [ ] **Step 6: Commit**

```bash
git add gen.go
git commit -m "feat(gen): route slice/array int/uint kinds to generic Int field"
```

---

### Task 6: Delete the six width-specific functions

**Files:**
- Modify: `gen.go` — delete `emitCborMarshalUint64Field` (line 576), `emitCborMarshalUint8Field` (line 592), `emitCborMarshalInt64Field` (line 601), `emitCborUnmarshalUint64Field` (line 1174), `emitCborUnmarshalUint8Field` (line 1210), `emitCborUnmarshalInt64Field` (line 1133).

**Interfaces:**
- Consumes: all eight switches from Tasks 3-5 no longer reference these functions.

- [ ] **Step 1: Delete the three marshal functions**

Remove these three complete functions from `gen.go`:

```go
func (g Gen) emitCborMarshalUint64Field(w io.Writer, f Field) error { ... }   // line 576-590
func (g Gen) emitCborMarshalUint8Field(w io.Writer, f Field) error { ... }    // line 592-599
func (g Gen) emitCborMarshalInt64Field(w io.Writer, f Field) error { ... }    // line 601-625
```

- [ ] **Step 2: Delete the three unmarshal functions**

Remove these three complete functions from `gen.go`:

```go
func (g Gen) emitCborUnmarshalInt64Field(w io.Writer, f Field) error { ... }  // line 1133-1172
func (g Gen) emitCborUnmarshalUint64Field(w io.Writer, f Field) error { ... } // line 1174-1208
func (g Gen) emitCborUnmarshalUint8Field(w io.Writer, f Field) error { ... }  // line 1210-1224
```

- [ ] **Step 3: Verify it compiles (no dangling references)**

Run: `go build ./...`
Expected: no errors. If a reference error appears, find the missed switch and update it to use the generic function.

- [ ] **Step 4: Commit**

```bash
git add gen.go
git commit -m "refactor(gen): remove width-specific Int marshal/unmarshal functions"
```

---

### Task 7: Add `AllInts` fixture type

**Files:**
- Modify: `testing/types.go` (append at end of file, after `SlicesOfArrays`)

**Interfaces:**
- Produces: `types.AllInts` struct used by Task 8 (registration) and Task 9 (tests).

- [ ] **Step 1: Add the type**

Append to `testing/types.go`:

```go
type AllInts struct {
	I8   int8
	I16  int16
	I32  int32
	I64  int64
	U8   uint8
	U16  uint16
	U32  uint32
	U64  uint64
	PI16 *int16
	PU32 *uint32
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./testing`
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add testing/types.go
git commit -m "test: add AllInts fixture type"
```

---

### Task 8: Register `AllInts` and regenerate fixtures

**Files:**
- Modify: `testgen/main.go` (the `WriteTupleEncodersToFile` call)
- Modify (regenerated): `testing/cbor_gen.go`

**Interfaces:**
- Consumes: `types.AllInts{}` from Task 7.
- Produces: regenerated `testing/cbor_gen.go` containing marshal/unmarshal methods for `AllInts`.

- [ ] **Step 1: Add the type to the generator invocation**

In `testgen/main.go`, add `types.AllInts{},` to the `WriteTupleEncodersToFile` argument list, after `types.SlicesOfArrays{},`:

```go
		types.TupleWithOptionalFields{},
		types.BinaryMarshalerTuple{},
		types.SlicesOfArrays{},
		types.AllInts{},
	); err != nil {
		panic(err)
	}
```

- [ ] **Step 2: Regenerate the test fixtures**

Run: `make gentest`
Expected: command exits 0; `testing/cbor_gen.go` is updated with `AllInts` marshal/unmarshal methods.

- [ ] **Step 3: Inspect the generated `AllInts` unmarshal code**

Open `testing/cbor_gen.go` and find the `func (t *AllInts) UnmarshalCBOR` method. Verify:

- The `U16` field has a check `if extra > 65535 { ... }`.
- The `I8` field has `if extra > 127` (positive) and `if extra > 127` (negative) checks.
- The `I16` field has `if extra > 32767` checks.

If the literal constants are present and correct, the overflow requirement is satisfied. If they are missing or wrong, stop and re-examine Task 2.

- [ ] **Step 4: Verify the generated code compiles**

Run: `go build ./testing`
Expected: no errors.

- [ ] **Step 5: Commit**

```bash
git add testgen/main.go testing/cbor_gen.go
git commit -m "test: regenerate fixtures with AllInts"
```

---

### Task 9: Add roundtrip and overflow tests

**Files:**
- Modify: `testing/roundtrip_test.go` (append after `TestSlicesOfArrays`)

**Interfaces:**
- Consumes: `types.AllInts` (Task 7), `testTypeRoundtrips` helper (`roundtrip_test.go:149`).

- [ ] **Step 1: Add the roundtrip test**

Insert after `TestSlicesOfArrays`:

```go
func TestAllInts(t *testing.T) {
	testTypeRoundtrips(t, reflect.TypeOf(AllInts{}))
}
```

- [ ] **Step 2: Run the roundtrip test**

Run: `go test ./testing -run TestAllInts -v`
Expected: PASS (1000 quickcheck iterations, covering positive/negative/nil values).

- [ ] **Step 3: Add overflow tests**

Append after `TestAllInts`:

```go
func TestAllIntsOverflow(t *testing.T) {
	t.Run("uint16 overflow", func(t *testing.T) {
		// CBOR for AllInts with U16 = 65536 (0x1 00 00 — major type 0, 2-byte payload).
		// Easiest: marshal a crafted value via a hand-built CBOR stream is verbose,
		// so we marshal a valid AllInts then overwrite the U16 slot. Instead, build
		// the full struct encoding by hand using the known field order.
		//
		// AllInts has 10 fields. Encoding is an array of 10 elements.
		// We construct: 0x8a (array of 10), then zeros for I8..U8, then 0x1a 00 01 00 00
		// (uint16 = 65536 as a 4-byte unsigned), then zeros for U32/U64/PI16/PU32.
		enc := []byte{
			0x8a, // array of 10
			0x00, // I8 = 0
			0x00, // I16 = 0
			0x00, // I32 = 0
			0x00, // I64 = 0
			0x00, // U8 = 0
			0x1a, 0x00, 0x01, 0x00, 0x00, // U16 = 65536 (too large)
			0x00, // U32 = 0
			0x00, // U64 = 0
			0xf6, // PI16 = nil
			0xf6, // PU32 = nil
		}
		var out AllInts
		err := out.UnmarshalCBOR(bytes.NewReader(enc))
		if err == nil {
			t.Fatal("expected overflow error for uint16, got nil")
		}
	})

	t.Run("int8 positive overflow", func(t *testing.T) {
		enc := []byte{
			0x8a,
			0x18, 0x80, // I8 = 128 (too large, encoded as major type 0, 1-byte payload 0x80)
			0x00, // I16
			0x00, // I32
			0x00, // I64
			0x00, // U8
			0x00, // U16
			0x00, // U32
			0x00, // U64
			0xf6, // PI16
			0xf6, // PU32
		}
		var out AllInts
		err := out.UnmarshalCBOR(bytes.NewReader(enc))
		if err == nil {
			t.Fatal("expected overflow error for int8, got nil")
		}
	})

	t.Run("int8 negative overflow", func(t *testing.T) {
		// -129 encodes as major type 1, payload 128 (0x38 0x80).
		enc := []byte{
			0x8a,
			0x38, 0x80, // I8 = -129 (too negative)
			0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			0xf6, 0xf6,
		}
		var out AllInts
		err := out.UnmarshalCBOR(bytes.NewReader(enc))
		if err == nil {
			t.Fatal("expected negative overflow error for int8, got nil")
		}
	})
}
```

- [ ] **Step 4: Run the overflow tests**

Run: `go test ./testing -run TestAllIntsOverflow -v`
Expected: all three subtests PASS (each returns an error).

If any subtest does not return an error, the overflow check is missing — return to Task 2 and verify the literal constants.

- [ ] **Step 5: Run the full test suite**

Run: `make test`
Expected: all tests pass.

- [ ] **Step 6: Commit**

```bash
git add testing/roundtrip_test.go
git commit -m "test: roundtrip and overflow tests for AllInts"
```
