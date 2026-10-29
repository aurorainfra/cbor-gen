package typegen

import (
	"bytes"
	"math"
	"testing"
)

func TestReadFloatWidths(t *testing.T) {
	for _, tc := range []struct {
		in   []byte
		want float64
	}{
		{[]byte{0xf9, 0x3c, 0x00}, 1.0},
		{[]byte{0xfa, 0x3f, 0xc0, 0x00, 0x00}, 1.5},
		{[]byte{0xfb, 0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2d, 0x18}, 3.141592653589793},
		{[]byte{0xf9, 0x00, 0x01}, math.Ldexp(1, -24)}, // smallest half subnormal
		{[]byte{0xf9, 0xfc, 0x00}, math.Inf(-1)},
		{[]byte{0xfa, 0x00, 0x00, 0x00, 0x00}, 0},
	} {
		got, err := ReadFloat(NewCborReader(bytes.NewReader(tc.in)))
		if err != nil {
			t.Fatalf("%x: %v", tc.in, err)
		}
		if math.Float64bits(got) != math.Float64bits(tc.want) {
			t.Fatalf("%x: got %v, want %v", tc.in, got, tc.want)
		}
	}

	var buf bytes.Buffer
	if err := WriteFloat64(&buf, 3.141592653589793); err != nil {
		t.Fatal(err)
	}
	if want := []byte{0xfb, 0x40, 0x09, 0x21, 0xfb, 0x54, 0x44, 0x2d, 0x18}; !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("WriteFloat64: got %x, want %x", buf.Bytes(), want)
	}
	buf.Reset()
	if err := WriteFloat32(&buf, 1.5); err != nil {
		t.Fatal(err)
	}
	if want := []byte{0xfa, 0x3f, 0xc0, 0x00, 0x00}; !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("WriteFloat32: got %x, want %x", buf.Bytes(), want)
	}

	if _, err := ReadFloat(NewCborReader(bytes.NewReader([]byte{0x01}))); err == nil {
		t.Fatal("expected error reading an int as float")
	}
}

// Floats whose bits look like a non-minimal integer header (e.g. 0.0) must
// still be skippable as unknown map values.
func TestScanForLinksSkipsFloats(t *testing.T) {
	in := []byte{
		0x83,
		0xf9, 0x00, 0x00,
		0xfa, 0x00, 0x00, 0x00, 0x00,
		0xfb, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
		0x07,
	}
	r := bytes.NewReader(in)
	if err := ScanForLinks(r, nil); err != nil {
		t.Fatal(err)
	}
	if r.Len() != 1 {
		t.Fatalf("expected 1 trailing byte, got %d", r.Len())
	}
}
