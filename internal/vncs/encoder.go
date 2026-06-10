// Package vncs is the pure-Go equivalent of Mesa's Venus command-stream
// encoder primitives (src/virtio/venus-protocol/vn_protocol_driver_cs.h,
// vn_protocol_driver_types.h and src/virtio/vulkan/vn_cs.h). It is the
// runtime that the generated Venus encoder functions call into, exactly as
// the Mesa-generated C calls into vn_encode_* / vn_cs_encoder_write.
//
// Every primitive here is transcribed from the Mesa source. The Mesa
// citations live next to each method so the wire format can be audited
// against upstream without a GPU or a host.
//
// Wire format (all confirmed from Mesa source — see method comments):
//
//   - Little-endian. vn_cs_encoder_write is a raw memcpy of native-endian
//     bytes (vn_cs.h); Venus runs guest and renderer on the same byte
//     order, so the wire is the host byte order, which is LE on every
//     platform Venus targets. We encode LE explicitly here.
//   - 4-byte alignment. vn_encode() asserts size%4==0
//     (vn_protocol_driver_cs.h). Every primitive writes a multiple of 4
//     bytes; sub-dword blobs/strings are zero-padded up to (size+3)&^3.
//   - uint64 / array_size are 8 bytes; everything scalar is 4 bytes.
package vncs

import (
	"encoding/binary"
	"math"
)

// Encoder is the pure-Go analogue of `struct vn_cs_encoder` plus the
// vn_cs_encoder_write write-pointer. Unlike Mesa we grow a []byte instead
// of writing into a fixed shmem window, but the bytes produced are
// identical.
type Encoder struct {
	buf []byte
}

// NewEncoder returns an empty Encoder.
func NewEncoder() *Encoder { return &Encoder{} }

// Bytes returns the encoded command stream so far.
func (e *Encoder) Bytes() []byte { return e.buf }

// Len returns the number of bytes encoded so far.
func (e *Encoder) Len() int { return len(e.buf) }

// write is the equivalent of vn_encode() + vn_cs_encoder_write():
//
//	static inline void
//	vn_encode(struct vn_cs_encoder *enc, size_t size, const void *data, size_t data_size)
//	{
//	   assert(size % 4 == 0);
//	   vn_cs_encoder_write(enc, size, data, data_size);
//	}
//
//	// vn_cs.h
//	assert(val_size <= size);
//	memcpy(enc->cur, val, val_size);
//	enc->cur += size;
//
// data is copied verbatim and the cursor advances by the padded size
// (a multiple of 4); padding bytes are left zero, matching the renderer's
// expectation that only data_size bytes are meaningful.
func (e *Encoder) write(size int, data []byte) {
	if size%4 != 0 {
		// Mirrors Mesa's `assert(size % 4 == 0)`. The generator only ever
		// feeds padded sizes, so this is an invariant guard.
		panic("vncs: write size not 4-byte aligned")
	}
	if len(data) > size {
		// Mirrors Mesa's `assert(val_size <= size)`.
		panic("vncs: data_size exceeds padded size")
	}
	start := len(e.buf)
	e.buf = append(e.buf, make([]byte, size)...)
	copy(e.buf[start:], data)
}

// EncodeUint32 mirrors vn_encode_uint32_t:
//
//	vn_encode(enc, 4, val, sizeof(*val));
func (e *Encoder) EncodeUint32(v uint32) {
	var b [4]byte
	binary.LittleEndian.PutUint32(b[:], v)
	e.write(4, b[:])
}

// EncodeInt32 mirrors vn_encode_int32_t:
//
//	vn_encode(enc, 4, val, sizeof(*val));
//
// VkStructureType, VkCommandTypeEXT and other enums are encoded through
// this path (vn_encode_VkStructureType casts to int32 and calls
// vn_encode_int32_t).
func (e *Encoder) EncodeInt32(v int32) {
	e.EncodeUint32(uint32(v))
}

// EncodeUint64 mirrors vn_encode_uint64_t:
//
//	vn_encode(enc, 8, val, sizeof(*val));
func (e *Encoder) EncodeUint64(v uint64) {
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	e.write(8, b[:])
}

// EncodeFloat32 mirrors vn_encode_float:
//
//	vn_encode(enc, 4, val, sizeof(*val));
//
// The IEEE-754 bit pattern is written little-endian, identical to the
// memcpy of a float on an LE host.
func (e *Encoder) EncodeFloat32(v float32) {
	e.EncodeUint32(math.Float32bits(v))
}

// EncodeFlags mirrors vn_encode_VkFlags (typedef uint32_t VkFlags):
//
//	vn_encode_VkFlags -> vn_encode_uint32_t.
func (e *Encoder) EncodeFlags(v uint32) {
	e.EncodeUint32(v)
}

// EncodeArraySize mirrors vn_encode_array_size:
//
//	static inline void
//	vn_encode_array_size(struct vn_cs_encoder *enc, uint64_t size)
//	{
//	    vn_encode_uint64_t(enc, &size);
//	}
//
// i.e. a dynamic-array element count / byte count is an 8-byte LE prefix.
func (e *Encoder) EncodeArraySize(size uint64) {
	e.EncodeUint64(size)
}

// EncodeSimplePointer mirrors vn_encode_simple_pointer:
//
//	static inline bool
//	vn_encode_simple_pointer(struct vn_cs_encoder *enc, const void *val)
//	{
//	    vn_encode_array_size(enc, val ? 1 : 0);
//	    return val;
//	}
//
// An optional pointer is a presence flag encoded as array_size(1) when
// present or array_size(0) when NULL — i.e. an 8-byte LE 1 or 0. The
// returned bool lets the caller conditionally encode the pointee, exactly
// as `if (vn_encode_simple_pointer(enc, p)) vn_encode_X(enc, p);`.
func (e *Encoder) EncodeSimplePointer(present bool) bool {
	if present {
		e.EncodeArraySize(1)
	} else {
		e.EncodeArraySize(0)
	}
	return present
}

// EncodeBlobArray mirrors vn_encode_blob_array:
//
//	static inline void
//	vn_encode_blob_array(struct vn_cs_encoder *enc, const void *val, size_t size)
//	{
//	    vn_encode(enc, (size + 3) & ~3, val, size);
//	}
//
// The blob is written verbatim and the cursor advances by the size rounded
// up to a multiple of 4; the trailing 0..3 pad bytes are zero.
func (e *Encoder) EncodeBlobArray(data []byte) {
	padded := (len(data) + 3) &^ 3
	e.write(padded, data)
}

// EncodeString mirrors the Venus convention for a `const char*` member,
// e.g. VkApplicationInfo.pApplicationName in
// vn_encode_VkApplicationInfo_self:
//
//	const size_t string_size = strlen(val->pApplicationName) + 1;
//	vn_encode_array_size(enc, string_size);
//	vn_encode_char_array(enc, val->pApplicationName, string_size);
//
// where vn_encode_char_array == vn_encode_blob_array. So a string is an
// 8-byte LE length prefix of strlen+1 (the NUL is counted and emitted),
// followed by the NUL-terminated bytes zero-padded to a 4-byte boundary.
//
// The Mesa precondition `assert(size && strlen(val) < size)` is honoured:
// the input Go string must not contain an interior NUL.
func (e *Encoder) EncodeString(s string) {
	for i := 0; i < len(s); i++ {
		if s[i] == 0 {
			// Mesa's vn_encode_char_array asserts strlen(val) < size,
			// which an interior NUL would violate.
			panic("vncs: interior NUL in string")
		}
	}
	stringSize := uint64(len(s) + 1) // strlen + 1 (counts the NUL)
	e.EncodeArraySize(stringSize)
	withNUL := make([]byte, len(s)+1)
	copy(withNUL, s)
	e.EncodeBlobArray(withNUL)
}

// EncodeHandle mirrors vn_encode_VkInstance (and every other dispatchable
// handle):
//
//	const uint64_t id = vn_cs_handle_load_id(...);
//	vn_encode_uint64_t(enc, &id);
//
// On the wire a handle is just its 8-byte LE object id. M0 never owns a
// real device, so the id is supplied by the caller (0 for a NULL handle,
// matching vn_cs_handle_load_id returning 0 for a NULL pointer).
func (e *Encoder) EncodeHandle(id uint64) {
	e.EncodeUint64(id)
}

// EncodeBool32 mirrors vn_encode_VkBool32 (typedef uint32_t VkBool32):
//
//	static inline void
//	vn_encode_VkBool32(struct vn_cs_encoder *enc, const VkBool32 *val)
//	{
//	    vn_encode_uint32_t(enc, val);
//	}
//
// i.e. a 4-byte LE uint32, 0 or 1.
func (e *Encoder) EncodeBool32(v bool) {
	if v {
		e.EncodeUint32(1)
	} else {
		e.EncodeUint32(0)
	}
}

// EncodeDeviceSize mirrors vn_encode_VkDeviceSize (typedef uint64_t
// VkDeviceSize):
//
//	static inline void
//	vn_encode_VkDeviceSize(struct vn_cs_encoder *enc, const VkDeviceSize *val)
//	{
//	    vn_encode_uint64_t(enc, val);
//	}
//
// i.e. an 8-byte LE uint64. VkDeviceAddress is the same path.
func (e *Encoder) EncodeDeviceSize(v uint64) {
	e.EncodeUint64(v)
}

// EncodeUint8Array mirrors vn_encode_uint8_t_array, used for fixed-size
// byte fields such as VkPhysicalDeviceProperties.pipelineCacheUUID[16]:
//
//	const size_t size = sizeof(*val) * count;
//	vn_encode(enc, size, val, size);
//
// Because uint8 has size 1, the byte count equals the element count; the
// caller pre-pads to a 4-byte multiple exactly as the fixed-array sizes
// (VK_UUID_SIZE == 16, etc.) always are. In Venus a fixed array is preceded
// by its own vn_encode_array_size(N); that prefix is emitted by the
// generated struct encoder, not here.
func (e *Encoder) EncodeUint8Array(data []byte) {
	e.write((len(data)+3)&^3, data)
}

// EncodeUint32Array mirrors vn_encode_uint32_t_array:
//
//	const size_t size = sizeof(*val) * count;  // 4*count
//	vn_encode(enc, size, val, size);
//
// A contiguous run of 4-byte LE dwords; size is already a multiple of 4.
func (e *Encoder) EncodeUint32Array(vals []uint32) {
	for _, v := range vals {
		e.EncodeUint32(v)
	}
}

// EncodeInt32Array mirrors vn_encode_int32_t_array (same wire shape as
// uint32_array; used by the VkClearColorValue int32 union arm).
func (e *Encoder) EncodeInt32Array(vals []int32) {
	for _, v := range vals {
		e.EncodeInt32(v)
	}
}

// EncodeFloat32Array mirrors vn_encode_float_array (used by the
// VkClearColorValue float32 union arm).
func (e *Encoder) EncodeFloat32Array(vals []float32) {
	for _, v := range vals {
		e.EncodeFloat32(v)
	}
}

// PNextNode is one node of a Vulkan pNext extension chain: its VkStructureType
// (encoded as int32, the value the parent's accepted-sType switch keys on) and
// a closure that encodes the node's "self" members (everything but sType/pNext,
// in declaration order). Generated chain-bearing struct encoders build a
// []PNextNode and hand it to EncodePNextChain.
type PNextNode struct {
	SType      int32
	EncodeSelf func(*Encoder)
}

// EncodePNextChain mirrors Mesa's generated vn_encode_<Struct>_pnext walk
// (e.g. vn_encode_VkSubmitInfo_pnext, vn_encode_VkImageMemoryBarrier_pnext):
//
//	const VkBaseInStructure *pnext = val;
//	while (pnext) {
//	    switch ((int32_t)pnext->sType) {
//	    case <KNOWN_STYPE>:
//	        vn_encode_simple_pointer(enc, pnext);            // present = 1
//	        vn_encode_VkStructureType(enc, &pnext->sType);   // sType int32
//	        vn_encode_<Struct>_pnext(enc, node->pNext);      // RECURSE on the rest
//	        vn_encode_<Node>_self(enc, node);                // THEN this node's self
//	        return;
//	    default:
//	        break;                                           // skip unknown sType
//	    }
//	    pnext = pnext->pNext;
//	}
//	vn_encode_simple_pointer(enc, NULL);                     // end of chain = 0
//
// Crucially the recursion happens BEFORE the node's self, so for a chain
// [A, B] the wire is:
//
//	sp(1) sTypeA  sp(1) sTypeB  sp(0)  selfB  selfA
//
// i.e. the per-node presence-flag+sType prefixes nest outermost-first while
// the self payloads unwind innermost-first. An empty chain is a single sp(0).
// Each node in nodes is treated as a "known" sType (the caller supplies only
// supported nodes), matching the case arm; Mesa's default (skip-unknown) arm
// is the responsibility of the caller, which simply omits unsupported nodes.
func (e *Encoder) EncodePNextChain(nodes []PNextNode) {
	if len(nodes) == 0 {
		e.EncodeSimplePointer(false) // end of chain
		return
	}
	head := nodes[0]
	e.EncodeSimplePointer(true)   // this node is present
	e.EncodeInt32(head.SType)     // vn_encode_VkStructureType
	e.EncodePNextChain(nodes[1:]) // recurse on the rest of the chain
	head.EncodeSelf(e)            // then this node's self members
}
