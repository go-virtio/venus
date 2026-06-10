package vncs

import (
	"bytes"
	"testing"
)

// Each case below derives its expected bytes from the Mesa primitive cited
// in the matching Encoder method, computed by hand. All multi-byte values
// are little-endian (raw memcpy on an LE host, vn_cs_encoder_write).

func TestEncodeUint32(t *testing.T) {
	e := NewEncoder()
	e.EncodeUint32(0x04030201)
	// vn_encode(enc, 4, &0x04030201, 4) -> LE dword
	want := []byte{0x01, 0x02, 0x03, 0x04}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeInt32(t *testing.T) {
	e := NewEncoder()
	e.EncodeInt32(-1)
	want := []byte{0xff, 0xff, 0xff, 0xff}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeUint64(t *testing.T) {
	e := NewEncoder()
	e.EncodeUint64(0x0807060504030201)
	want := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeFloat32(t *testing.T) {
	e := NewEncoder()
	// 1.0f == 0x3f800000 (IEEE-754); LE on the wire.
	e.EncodeFloat32(1.0)
	want := []byte{0x00, 0x00, 0x80, 0x3f}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeFlags(t *testing.T) {
	e := NewEncoder()
	e.EncodeFlags(0xABCD1234)
	want := []byte{0x34, 0x12, 0xcd, 0xab}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeArraySize(t *testing.T) {
	e := NewEncoder()
	// vn_encode_array_size -> vn_encode_uint64_t: 8-byte LE.
	e.EncodeArraySize(5)
	want := []byte{0x05, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeSimplePointer(t *testing.T) {
	// present -> array_size(1); absent -> array_size(0). Both 8-byte LE.
	ePresent := NewEncoder()
	if got := ePresent.EncodeSimplePointer(true); got != true {
		t.Fatalf("present return = %v", got)
	}
	if want := []byte{1, 0, 0, 0, 0, 0, 0, 0}; !bytes.Equal(ePresent.Bytes(), want) {
		t.Fatalf("present got % x want % x", ePresent.Bytes(), want)
	}
	eAbsent := NewEncoder()
	if got := eAbsent.EncodeSimplePointer(false); got != false {
		t.Fatalf("absent return = %v", got)
	}
	if want := []byte{0, 0, 0, 0, 0, 0, 0, 0}; !bytes.Equal(eAbsent.Bytes(), want) {
		t.Fatalf("absent got % x want % x", eAbsent.Bytes(), want)
	}
}

func TestEncodeBlobArray(t *testing.T) {
	e := NewEncoder()
	// 5 bytes -> padded to (5+3)&~3 = 8; 3 zero pad bytes.
	e.EncodeBlobArray([]byte{1, 2, 3, 4, 5})
	want := []byte{1, 2, 3, 4, 5, 0, 0, 0}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
	// Already-aligned blob: no padding.
	e2 := NewEncoder()
	e2.EncodeBlobArray([]byte{1, 2, 3, 4})
	if want := []byte{1, 2, 3, 4}; !bytes.Equal(e2.Bytes(), want) {
		t.Fatalf("aligned got % x want % x", e2.Bytes(), want)
	}
	// Empty blob: (0+3)&~3 = 0 bytes.
	e3 := NewEncoder()
	e3.EncodeBlobArray(nil)
	if e3.Len() != 0 {
		t.Fatalf("empty blob wrote %d bytes", e3.Len())
	}
}

func TestEncodeString(t *testing.T) {
	e := NewEncoder()
	// "Vk" -> string_size = strlen+1 = 3; array_size(3) [8 LE] then
	// blob_array of {'V','k',0} padded to 4 = {'V','k',0,0}.
	e.EncodeString("Vk")
	want := []byte{
		3, 0, 0, 0, 0, 0, 0, 0, // array_size = 3
		'V', 'k', 0, 0, // "Vk\0" padded to 4
	}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeStringEmpty(t *testing.T) {
	e := NewEncoder()
	// "" -> string_size = 1; array_size(1) then {0} padded to 4 = {0,0,0,0}.
	e.EncodeString("")
	want := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeHandle(t *testing.T) {
	e := NewEncoder()
	e.EncodeHandle(0x1122334455667788)
	want := []byte{0x88, 0x77, 0x66, 0x55, 0x44, 0x33, 0x22, 0x11}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeBool32(t *testing.T) {
	// vn_encode_VkBool32 -> vn_encode_uint32_t: 0 or 1, 4-byte LE.
	eT := NewEncoder()
	eT.EncodeBool32(true)
	if want := []byte{1, 0, 0, 0}; !bytes.Equal(eT.Bytes(), want) {
		t.Fatalf("true got % x want % x", eT.Bytes(), want)
	}
	eF := NewEncoder()
	eF.EncodeBool32(false)
	if want := []byte{0, 0, 0, 0}; !bytes.Equal(eF.Bytes(), want) {
		t.Fatalf("false got % x want % x", eF.Bytes(), want)
	}
}

func TestEncodeDeviceSize(t *testing.T) {
	// vn_encode_VkDeviceSize -> vn_encode_uint64_t: 8-byte LE.
	e := NewEncoder()
	e.EncodeDeviceSize(0x1000)
	want := []byte{0x00, 0x10, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeUint8Array(t *testing.T) {
	// A 16-byte UUID-style field: exact multiple of 4, no padding.
	e := NewEncoder()
	uuid := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}
	e.EncodeUint8Array(uuid)
	if !bytes.Equal(e.Bytes(), uuid) {
		t.Fatalf("uuid got % x", e.Bytes())
	}
	// A 5-byte field pads to 8 (mirrors blob_array padding on the encode
	// side for fixed sub-dword arrays).
	e2 := NewEncoder()
	e2.EncodeUint8Array([]byte{1, 2, 3, 4, 5})
	if want := []byte{1, 2, 3, 4, 5, 0, 0, 0}; !bytes.Equal(e2.Bytes(), want) {
		t.Fatalf("padded got % x want % x", e2.Bytes(), want)
	}
}

func TestEncodeUint32Array(t *testing.T) {
	e := NewEncoder()
	e.EncodeUint32Array([]uint32{1, 0x04030201})
	want := []byte{1, 0, 0, 0, 0x01, 0x02, 0x03, 0x04}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeInt32Array(t *testing.T) {
	e := NewEncoder()
	e.EncodeInt32Array([]int32{-1, 2})
	want := []byte{0xff, 0xff, 0xff, 0xff, 2, 0, 0, 0}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestEncodeFloat32Array(t *testing.T) {
	e := NewEncoder()
	e.EncodeFloat32Array([]float32{1.0, 0.0})
	want := []byte{0x00, 0x00, 0x80, 0x3f, 0, 0, 0, 0}
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("got % x want % x", e.Bytes(), want)
	}
}

func TestLenTracksBytes(t *testing.T) {
	e := NewEncoder()
	e.EncodeUint32(1)
	e.EncodeUint64(2)
	if e.Len() != 12 {
		t.Fatalf("Len = %d want 12", e.Len())
	}
}

func TestWritePanicsOnUnaligned(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on unaligned size")
		}
	}()
	NewEncoder().write(3, []byte{1, 2, 3})
}

func TestWritePanicsOnOverflow(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when data exceeds padded size")
		}
	}()
	NewEncoder().write(4, []byte{1, 2, 3, 4, 5})
}

func TestEncodeStringPanicsOnInteriorNUL(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on interior NUL")
		}
	}()
	NewEncoder().EncodeString("a\x00b")
}

// le32b/le64b build little-endian dwords/qwords for the hand-derived pNext
// chain streams below (kept local so the vncs tests stay self-contained).
func le32b(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
func le64b(v uint64) []byte {
	return []byte{
		byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24),
		byte(v >> 32), byte(v >> 40), byte(v >> 48), byte(v >> 56),
	}
}

// TestEncodePNextChain validates EncodePNextChain against bytes hand-derived
// from Mesa's vn_encode_<Struct>_pnext walk for the empty, 1-node and 2-node
// cases. The recursion-before-self nesting is the load-bearing detail: the
// per-node sp(1)+sType prefixes nest outermost-first while the self payloads
// unwind innermost-first.
func TestEncodePNextChain(t *testing.T) {
	// Empty chain: a single simple_pointer(NULL) == array_size(0) == LE 0 qword.
	eEmpty := NewEncoder()
	eEmpty.EncodePNextChain(nil)
	if !bytes.Equal(eEmpty.Bytes(), le64b(0)) {
		t.Fatalf("empty chain\n got % x\nwant % x", eEmpty.Bytes(), le64b(0))
	}

	// 1-node chain: node A (sType 1000314000, self = one uint32 0xAA).
	//   sp(1)  sTypeA  [recurse(nil): sp(0)]  selfA
	const sTypeA = int32(1000314000)
	nodeA := PNextNode{SType: sTypeA, EncodeSelf: func(e *Encoder) { e.EncodeUint32(0xAA) }}
	e1 := NewEncoder()
	e1.EncodePNextChain([]PNextNode{nodeA})
	var want1 []byte
	want1 = append(want1, le64b(1)...)              // sp(1): node A present
	want1 = append(want1, le32b(uint32(sTypeA))...) // sTypeA
	want1 = append(want1, le64b(0)...)              // recurse(nil): sp(0) end of chain
	want1 = append(want1, le32b(0xAA)...)           // selfA
	if !bytes.Equal(e1.Bytes(), want1) {
		t.Fatalf("1-node chain\n got % x\nwant % x", e1.Bytes(), want1)
	}

	// 2-node chain A->B: B's sType 1000207000, self = one uint64 0xBBBB.
	//   sp(1) sTypeA  sp(1) sTypeB  sp(0)  selfB  selfA
	const sTypeB = int32(1000207000)
	nodeB := PNextNode{SType: sTypeB, EncodeSelf: func(e *Encoder) { e.EncodeUint64(0xBBBB) }}
	e2 := NewEncoder()
	e2.EncodePNextChain([]PNextNode{nodeA, nodeB})
	var want2 []byte
	want2 = append(want2, le64b(1)...)              // sp(1): node A present
	want2 = append(want2, le32b(uint32(sTypeA))...) // sTypeA
	want2 = append(want2, le64b(1)...)              // sp(1): node B present (recursion)
	want2 = append(want2, le32b(uint32(sTypeB))...) // sTypeB
	want2 = append(want2, le64b(0)...)              // sp(0): end of chain
	want2 = append(want2, le64b(0xBBBB)...)         // selfB (innermost unwinds first)
	want2 = append(want2, le32b(0xAA)...)           // selfA
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("2-node chain\n got % x\nwant % x", e2.Bytes(), want2)
	}
}
