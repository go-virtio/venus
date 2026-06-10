package vncs

import (
	"bytes"
	"testing"
)

// Each case derives its INPUT bytes from the Mesa primitive cited in the
// matching Decoder method, computed by hand (little-endian, 4-byte aligned),
// and asserts the decoded value. The streams are NOT produced by the
// Encoder, so encode and decode are validated independently.

func TestDecodeUint32(t *testing.T) {
	d := NewDecoder([]byte{0x01, 0x02, 0x03, 0x04})
	if got := d.DecodeUint32(); got != 0x04030201 {
		t.Fatalf("got %#x want %#x", got, 0x04030201)
	}
	if d.Remaining() != 0 || d.Fatal() {
		t.Fatalf("remaining=%d fatal=%v", d.Remaining(), d.Fatal())
	}
}

func TestDecodeInt32(t *testing.T) {
	d := NewDecoder([]byte{0xff, 0xff, 0xff, 0xff})
	if got := d.DecodeInt32(); got != -1 {
		t.Fatalf("got %d want -1", got)
	}
}

func TestDecodeUint64(t *testing.T) {
	d := NewDecoder([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08})
	if got := d.DecodeUint64(); got != 0x0807060504030201 {
		t.Fatalf("got %#x", got)
	}
}

func TestDecodeFloat32(t *testing.T) {
	// 1.0f == 0x3f800000, LE on the wire.
	d := NewDecoder([]byte{0x00, 0x00, 0x80, 0x3f})
	if got := d.DecodeFloat32(); got != 1.0 {
		t.Fatalf("got %v want 1.0", got)
	}
}

func TestDecodeFlags(t *testing.T) {
	d := NewDecoder([]byte{0x34, 0x12, 0xcd, 0xab})
	if got := d.DecodeFlags(); got != 0xABCD1234 {
		t.Fatalf("got %#x", got)
	}
}

func TestDecodeBool32(t *testing.T) {
	if !NewDecoder([]byte{1, 0, 0, 0}).DecodeBool32() {
		t.Fatal("1 -> true")
	}
	if NewDecoder([]byte{0, 0, 0, 0}).DecodeBool32() {
		t.Fatal("0 -> false")
	}
	if !NewDecoder([]byte{0, 0, 0, 0x80}).DecodeBool32() {
		t.Fatal("nonzero -> true")
	}
}

func TestDecodeDeviceSize(t *testing.T) {
	d := NewDecoder([]byte{0x00, 0x10, 0, 0, 0, 0, 0, 0})
	if got := d.DecodeDeviceSize(); got != 0x1000 {
		t.Fatalf("got %#x", got)
	}
}

func TestDecodeResult(t *testing.T) {
	// VK_SUCCESS == 0; VK_ERROR_OUT_OF_HOST_MEMORY == -1.
	if got := NewDecoder([]byte{0, 0, 0, 0}).DecodeResult(); got != 0 {
		t.Fatalf("VK_SUCCESS got %d", got)
	}
	if got := NewDecoder([]byte{0xff, 0xff, 0xff, 0xff}).DecodeResult(); got != -1 {
		t.Fatalf("error result got %d", got)
	}
}

func TestDecodeArraySize(t *testing.T) {
	// In-range: array_size(5) bounded by max 10 -> 5, not fatal.
	d := NewDecoder([]byte{5, 0, 0, 0, 0, 0, 0, 0})
	if got := d.DecodeArraySize(10); got != 5 || d.Fatal() {
		t.Fatalf("got %d fatal=%v", got, d.Fatal())
	}
	// Over-range: array_size(9) bounded by max 4 -> 0 + fatal.
	d2 := NewDecoder([]byte{9, 0, 0, 0, 0, 0, 0, 0})
	if got := d2.DecodeArraySize(4); got != 0 || !d2.Fatal() {
		t.Fatalf("over-range got %d fatal=%v", got, d2.Fatal())
	}
}

func TestPeekArraySize(t *testing.T) {
	d := NewDecoder([]byte{3, 0, 0, 0, 0, 0, 0, 0, 0xaa, 0xbb, 0xcc, 0xdd})
	if got := d.PeekArraySize(); got != 3 {
		t.Fatalf("peek got %d", got)
	}
	// Peek must not advance: the next decode still sees the size prefix.
	if got := d.DecodeArraySize(3); got != 3 {
		t.Fatalf("decode after peek got %d", got)
	}
	if got := d.DecodeUint32(); got != 0xddccbbaa {
		t.Fatalf("post-array dword got %#x", got)
	}
	// Peek underflow -> 0 + fatal.
	short := NewDecoder([]byte{1, 2, 3})
	if got := short.PeekArraySize(); got != 0 || !short.Fatal() {
		t.Fatalf("peek underflow got %d fatal=%v", got, short.Fatal())
	}
}

func TestDecodeSimplePointer(t *testing.T) {
	if !NewDecoder([]byte{1, 0, 0, 0, 0, 0, 0, 0}).DecodeSimplePointer() {
		t.Fatal("present -> true")
	}
	if NewDecoder([]byte{0, 0, 0, 0, 0, 0, 0, 0}).DecodeSimplePointer() {
		t.Fatal("absent -> false")
	}
	// >1 is a protocol error: array_size bounded by 1 -> fatal, false.
	d := NewDecoder([]byte{2, 0, 0, 0, 0, 0, 0, 0})
	if d.DecodeSimplePointer() || !d.Fatal() {
		t.Fatal("simple_pointer(2) should be fatal/false")
	}
}

func TestDecodeBlobArray(t *testing.T) {
	// 5 payload bytes occupy (5+3)&~3 = 8 wire bytes; the trailing dword is
	// the next field, untouched.
	d := NewDecoder([]byte{1, 2, 3, 4, 5, 0, 0, 0, 0xaa, 0xbb, 0xcc, 0xdd})
	if got := d.DecodeBlobArray(5); !bytes.Equal(got, []byte{1, 2, 3, 4, 5}) {
		t.Fatalf("blob got % x", got)
	}
	if got := d.DecodeUint32(); got != 0xddccbbaa {
		t.Fatalf("post-blob dword got %#x", got)
	}
}

func TestDecodeHandle(t *testing.T) {
	d := NewDecoder([]byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11})
	if got := d.DecodeHandle(); got != 0x1122334455667788 {
		t.Fatalf("got %#x", got)
	}
}

// TestDecodeUnderflow exercises the short-stream (fatal) path of read: a
// 4-byte read against a 2-byte buffer yields a zero value and sets fatal.
func TestDecodeUnderflow(t *testing.T) {
	d := NewDecoder([]byte{1, 2})
	if got := d.DecodeUint32(); got != 0 {
		t.Fatalf("underflow uint32 got %#x", got)
	}
	if !d.Fatal() {
		t.Fatal("underflow should set fatal")
	}
	// uint64 underflow path (exercises pad8 + fatal).
	d2 := NewDecoder([]byte{1, 2, 3, 4})
	if got := d2.DecodeUint64(); got != 0 || !d2.Fatal() {
		t.Fatalf("uint64 underflow got %#x fatal=%v", got, d2.Fatal())
	}
	// blob underflow path.
	d3 := NewDecoder([]byte{1, 2})
	if got := d3.DecodeBlobArray(5); len(got) != 5 || !d3.Fatal() {
		t.Fatalf("blob underflow len=%d fatal=%v", len(got), d3.Fatal())
	}
}

func TestDecodeReadPanicsOnUnaligned(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on unaligned size")
		}
	}()
	NewDecoder([]byte{1, 2, 3, 4}).read(3, 3)
}

func TestDecodeReadPanicsOnOverflow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when data_size exceeds size")
		}
	}()
	NewDecoder([]byte{1, 2, 3, 4}).read(4, 5)
}

// TestRoundTripPrimitives confirms encode->decode is the identity for each
// scalar, an independent cross-check on top of the hand-derived byte tests.
func TestRoundTripPrimitives(t *testing.T) {
	e := NewEncoder()
	e.EncodeUint32(0xCAFEBABE)
	e.EncodeInt32(-7)
	e.EncodeUint64(0x0123456789ABCDEF)
	e.EncodeFloat32(3.5)
	e.EncodeBool32(true)
	e.EncodeDeviceSize(0x4000)
	e.EncodeHandle(0xDEAD)
	d := NewDecoder(e.Bytes())
	if d.DecodeUint32() != 0xCAFEBABE ||
		d.DecodeInt32() != -7 ||
		d.DecodeUint64() != 0x0123456789ABCDEF ||
		d.DecodeFloat32() != 3.5 ||
		!d.DecodeBool32() ||
		d.DecodeDeviceSize() != 0x4000 ||
		d.DecodeHandle() != 0xDEAD {
		t.Fatal("round-trip mismatch")
	}
	if d.Remaining() != 0 || d.Fatal() {
		t.Fatalf("remaining=%d fatal=%v", d.Remaining(), d.Fatal())
	}
}
