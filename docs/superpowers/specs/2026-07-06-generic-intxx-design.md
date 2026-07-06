# Generic IntXX Support — Design

## Problem

`cbor-gen` only supports `int64`, `uint64`, and `uint8` integer field kinds.
Structs containing `int8`, `int16`, `int32`, `uint16`, or `uint32` fields fail
to generate. The user wants the entire explicitly-sized `(u)intXX` family
supported, via a single generic function rather than copy-pasted per-width
functions.

## Goal

Replace the six width-specific marshal/unmarshal functions with two generic
functions — `emitCborMarshalIntField` and `emitCborUnmarshalIntField` — that
handle all of `int8/16/32/64` and `uint8/16/32/64`. Route all eight
`reflect.Int*` / `reflect.Uint*` kinds to them in every type switch.

## CBOR Conformance (RFC 8949 §3.1)

All explicitly-sized integer widths use the same CBOR major types:

- **Major type 0 (unsigned int)** for non-negative values, with the value
  carried in the "additional information" bits of the header.
- **Major type 1 (negative int)** for negative signed values, encoding
  `-1 - n` where `n >= 0`.

The "additional information" encoding (small value inline vs. 1/2/4/8-byte
following) is handled by the existing `MajorType` template helper and
`cbg.ReadHeader`; it is independent of the Go field width. The only
width-specific concern is **overflow checking on unmarshal**: a `uint16` field
must reject values > 65535; an `int8` field must reject values > 127 or
< -128.

## Overflow Checking (Critical Requirement)

**Every unmarshal path MUST return an error when the incoming CBOR value does
not fit in the target Go field's width.** This is the central correctness
property of the generic function.

The bit width is computed at generation time from `f.Type.Bits()` (e.g. 8, 16,
32, 64), and the overflow thresholds are emitted as **literal constants** in
the generated code — no runtime reflection.

### Unsigned kinds (`uint8/16/32/64`)

```
maxUnsigned = (1 << bits) - 1
```

- Require `maj == cbg.MajUnsignedInt`; any other major type → error
  `"wrong type for <typename> field"`.
- If `extra > maxUnsigned` → error
  `"<name>: value %d too large for <typename>"`.

### Signed kinds (`int8/16/32/64`)

```
maxPositive = (1 << (bits-1)) - 1     // e.g. int8: 127
maxNegative = (1 << (bits-1)) - 1     // e.g. int8: 127, encoded as -1-127 = -128
```

- `case cbg.MajUnsignedInt`: if `extra > maxPositive` → error
  `"<name>: value %d too large for <typename>"`. Else assign
  `TypeName(int64(extra))`.
- `case cbg.MajNegativeInt`: if `extra > maxNegative` → error
  `"<name>: value %d too negative for <typename>"`. Else compute
  `-1 - int64(extra)` and assign.
- Any other major type → error `"wrong type for <typename> field"`.

For `int64`/`uint64` the bit-width check is `extra > math.MaxInt64` etc., which
on a 64-bit platform reduces to the existing `int64(extra) < 0` overflow
sentinel check — the generated code keeps that pattern for the 64-bit case to
preserve the existing behavior and tests.

### Why literal constants are safe

`f.Type.Bits()` returns the width in bits (8, 16, 32, 64). At generation time
we format these into the template as integer literals, so the emitted Go source
contains checks like `if extra > 255` (uint8), `if extra > 32767` (int16
positive), `if extra > 32767` (int16 negative). The generated code has no
runtime reflection and is portable across 32/64-bit target machines.

## Architecture

### Marshal: `emitCborMarshalIntField(w, f)`

Single template branching on signedness, detected at generation time from
`f.Type.Kind()` (whether it's in the `reflect.Int*` set or `reflect.Uint*`
set). This is passed to the template as a `.Signed` boolean.

- **Unsigned path** — pointer-supported (nil → `cbg.CborNull`), else
  `{{ MajorType "cw" "cbg.MajUnsignedInt" .Name }}`. Matches current
  `Uint64` behavior.
- **Signed path** — pointer-supported; if `*Name >= 0` emit `MajUnsignedInt`,
  else emit `MajNegativeInt` with `-*Name-1`. Matches current `Int64`
  behavior.

The current `Uint8` function disallowed pointers; the generic version unifies
on pointer support, which is a minor, consistent behavior expansion. (Pointer
to `uint8` is unusual but valid and now uniformly supported.)

### Unmarshal: `emitCborUnmarshalIntField(w, f)`

Single template parameterized by `.Signed`, `.Bits`, `.MaxUnsigned`,
`.MaxPositive`, `.MaxNegative` (all computed at gen time from
`f.Type.Kind()` and `f.Type.Bits()`):

1. (If `.Pointer`) read one byte; if `cbg.CborNull[0]`, leave nil; else
   unread and fall through.
2. `maj, extra, err := ReadHeader(cr)`.
3. **Unsigned:** require `MajUnsignedInt`; check `extra > .MaxUnsigned`;
   assign `TypeName(extra)` (or `&typed` for pointer).
4. **Signed:** switch on `maj`:
   - `MajUnsignedInt`: check `extra > .MaxPositive`; assign
     `TypeName(int64(extra))`.
   - `MajNegativeInt`: check `extra > .MaxNegative`; compute
     `-1 - int64(extra)`; assign.
   - default: error.
5. (If `.Pointer`) close the `if` block.

### Switch updates

Six type switches currently dispatch `Uint64`/`Uint8`/`Int64` to the
per-width functions. Replace those three cases with a single multi-case
routing to the generic functions:

```go
case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
     reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
    if err := g.emitCborMarshalIntField(w, f); err != nil {
        return err
    }
```

(and the unmarshal equivalent). The six switches are:

1. `emitCborMarshalStructTuple` body — `gen.go:911`
2. `emitCborMarshalSliceField` subfield switch — `gen.go:768`
3. `emitCborMarshalArrayField` subfield switch — `gen.go:840`
4. `emitCborUnmarshalStructTuple` body — `gen.go:~1780`
5. `emitCborUnmarshalSliceField` subfield switch — `gen.go:1469`
6. `emitCborUnmarshalArrayField` subfield switch — `gen.go:1621`
7. `emitCborMarshalStructMap` body — `gen.go:1955`
8. `emitCborUnmarshalStructMap` body — `gen.go:2080`

(Eight total — the map struct switches also have the pattern.)

### Dead-code cleanup

The slice/array marshal subfield switches have an unreachable
`case reflect.Uint8` (the `[]byte` / `[N]byte` fast path returns earlier at
`gen.go:717` / `gen.go:808`). Remove those dead cases; the fast path stays.

### Deleted functions

- `emitCborMarshalUint64Field` (`gen.go:576`)
- `emitCborMarshalUint8Field` (`gen.go:592`)
- `emitCborMarshalInt64Field` (`gen.go:601`)
- `emitCborUnmarshalUint64Field` (`gen.go:1174`)
- `emitCborUnmarshalUint8Field` (`gen.go:1210`)
- `emitCborUnmarshalInt64Field` (`gen.go:1133`)

## Testing

### New fixture type

Add to `testing/types.go`:

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

Register in `testgen/main.go` via `WriteTupleEncodersToFile`.

### Roundtrip test

Add `TestAllInts` to `testing/roundtrip_test.go` using the existing
`testTypeRoundtrips` helper (1000 quickcheck iterations, covering negative and
positive values for signed kinds, nil and non-nil for the pointer fields).

### Overflow tests

Add explicit tests in `testing/roundtrip_test.go` that craft CBOR bytes
representating out-of-range values and assert the unmarshaler returns an
error. Example for `uint8` (already covered by existing tests) is extended to
`uint16` (value 65536 → error) and `int8` (value 128 → error; value -129 →
error). These verify the overflow-check requirement directly.

### Regenerate and run

`make gentest && make test` — all tests pass.

## Files Changed

- `gen.go` — add 2 generic functions, delete 6 width-specific functions,
  update 8 type switches, remove 2 dead `case reflect.Uint8` lines.
- `testing/types.go` — `AllInts` type.
- `testgen/main.go` — register `AllInts`.
- `testing/roundtrip_test.go` — `TestAllInts` plus overflow tests.
- `testing/cbor_gen.go` — regenerated.

## Out of Scope

- Platform-dependent `int`, `uint`, `uintptr` (excluded per scope decision —
  their size varies across machines, making generated code non-portable).
- Changing the `[]byte` / `[N]byte` fast paths (still special-cased to
  `MajByteString`).
- Refactoring the `MajorType` / `ReadHeader` template helpers.
