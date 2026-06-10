package vncs

import (
	"encoding/binary"
	"math"
)

// Decoder is the pure-Go analogue of `struct vn_cs_decoder` plus the
// vn_cs_decoder_read cursor. It reads a Venus reply (or any command) stream
// produced by an Encoder (or by Mesa's renderer), mirroring the decode
// primitives in vn_protocol_driver_types.h. Every primitive here is the
// inverse of the matching Encoder method and is cited against the Mesa
// vn_decode_* function it transcribes.
//
// Wire format is identical to the encoder (see encoder.go): little-endian,
// 4-byte aligned reads. vn_decode() asserts size%4==0 just like vn_encode()
// (vn_protocol_driver_cs.h):
//
//	static inline void
//	vn_decode(struct vn_cs_decoder *dec, size_t size, void *data, size_t data_size)
//	{
//	    assert(size % 4 == 0);
//	    vn_cs_decoder_read(dec, size, data, data_size);
//	}
type Decoder struct {
	buf   []byte
	pos   int
	fatal bool // mirrors vn_cs_decoder_set_fatal
}

// NewDecoder returns a Decoder positioned at the start of buf.
func NewDecoder(buf []byte) *Decoder { return &Decoder{buf: buf} }

// Remaining returns the number of undecoded bytes.
func (d *Decoder) Remaining() int { return len(d.buf) - d.pos }

// Fatal reports whether a decode bound check has failed
// (vn_cs_decoder_set_fatal). A well-formed, in-range stream never sets it.
func (d *Decoder) Fatal() bool { return d.fatal }

// read is the equivalent of vn_decode() + vn_cs_decoder_read(): it consumes
// size bytes (a multiple of 4) from the stream and returns the leading
// data_size of them. A short stream is a protocol violation; Mesa's
// vn_cs_decoder_read sets the decoder fatal and returns zeroed data. We
// mirror that: on underflow we mark fatal and return a zero slice of the
// requested data_size so callers decode a deterministic zero rather than
// panicking on a truncated reply.
func (d *Decoder) read(size, dataSize int) []byte {
	if size%4 != 0 {
		// Mirrors Mesa's `assert(size % 4 == 0)`.
		panic("vncs: read size not 4-byte aligned")
	}
	if dataSize > size {
		// Mirrors Mesa's `assert(val_size <= size)` on the read side.
		panic("vncs: data_size exceeds padded size")
	}
	if d.pos+size > len(d.buf) {
		// vn_cs_decoder_read: out-of-bounds read -> set fatal, yield zeros.
		d.fatal = true
		return make([]byte, dataSize)
	}
	out := d.buf[d.pos : d.pos+dataSize]
	d.pos += size
	return out
}

// DecodeUint32 mirrors vn_decode_uint32_t:
//
//	vn_decode(dec, 4, val, sizeof(*val));
//
// read always returns exactly data_size bytes (the underflow path yields a
// zeroed data_size slice), so the 4-byte LE load is always well-formed.
func (d *Decoder) DecodeUint32() uint32 {
	return binary.LittleEndian.Uint32(d.read(4, 4))
}

// DecodeInt32 mirrors vn_decode_int32_t. VkStructureType, VkCommandTypeEXT,
// VkResult and other enums decode through this path
// (vn_decode_VkResult casts to int32 and calls vn_decode_int32_t).
func (d *Decoder) DecodeInt32() int32 {
	return int32(d.DecodeUint32())
}

// DecodeUint64 mirrors vn_decode_uint64_t:
//
//	vn_decode(dec, 8, val, sizeof(*val));
func (d *Decoder) DecodeUint64() uint64 {
	return binary.LittleEndian.Uint64(d.read(8, 8))
}

// DecodeFloat32 mirrors vn_decode_float: an IEEE-754 bit pattern read LE.
func (d *Decoder) DecodeFloat32() float32 {
	return math.Float32frombits(d.DecodeUint32())
}

// DecodeFlags mirrors vn_decode_VkFlags -> vn_decode_uint32_t.
func (d *Decoder) DecodeFlags() uint32 { return d.DecodeUint32() }

// DecodeBool32 mirrors vn_decode_VkBool32 -> vn_decode_uint32_t. Any
// non-zero dword decodes as true.
func (d *Decoder) DecodeBool32() bool { return d.DecodeUint32() != 0 }

// DecodeDeviceSize mirrors vn_decode_VkDeviceSize -> vn_decode_uint64_t.
func (d *Decoder) DecodeDeviceSize() uint64 { return d.DecodeUint64() }

// DecodeResult mirrors vn_decode_VkResult -> vn_decode_int32_t.
func (d *Decoder) DecodeResult() int32 { return d.DecodeInt32() }

// DecodeArraySize mirrors vn_decode_array_size:
//
//	static inline uint64_t
//	vn_decode_array_size(struct vn_cs_decoder *dec, uint64_t max_size)
//	{
//	    uint64_t size;
//	    vn_decode_uint64_t(dec, &size);
//	    if (size > max_size) {
//	        vn_cs_decoder_set_fatal(dec);
//	        size = 0;
//	    }
//	    return size;
//	}
//
// i.e. an 8-byte LE prefix bounded by max_size; an over-large size is a
// protocol error that sets the decoder fatal and yields 0.
func (d *Decoder) DecodeArraySize(maxSize uint64) uint64 {
	size := d.DecodeUint64()
	if size > maxSize {
		d.fatal = true
		return 0
	}
	return size
}

// PeekArraySize mirrors vn_peek_array_size: read the next 8-byte LE
// array-size prefix without advancing the cursor. Venus reply decoders use
// it to choose between the present and absent arms of an out-array (e.g.
// vn_decode_vkEnumeratePhysicalDevices_reply). On underflow it yields 0.
func (d *Decoder) PeekArraySize() uint64 {
	if d.pos+8 > len(d.buf) {
		d.fatal = true
		return 0
	}
	return binary.LittleEndian.Uint64(d.buf[d.pos : d.pos+8])
}

// DecodeSimplePointer mirrors vn_decode_simple_pointer:
//
//	static inline bool
//	vn_decode_simple_pointer(struct vn_cs_decoder *dec)
//	{
//	    return vn_decode_array_size(dec, 1);
//	}
//
// i.e. an 8-byte LE presence flag (0 or 1); >1 is a protocol error.
func (d *Decoder) DecodeSimplePointer() bool {
	return d.DecodeArraySize(1) != 0
}

// DecodeBlobArray mirrors vn_decode_blob_array:
//
//	static inline void
//	vn_decode_blob_array(struct vn_cs_decoder *dec, void *val, size_t size)
//	{
//	    vn_decode(dec, (size + 3) & ~3, val, size);
//	}
//
// Reads size payload bytes, advancing the cursor past the 4-byte-padded
// span. The returned slice aliases the underlying buffer; callers that
// retain it should copy.
func (d *Decoder) DecodeBlobArray(size int) []byte {
	padded := (size + 3) &^ 3
	return d.read(padded, size)
}

// DecodeHandle mirrors vn_decode_VkInstance (and every dispatchable handle):
//
//	uint64_t id;
//	vn_decode_uint64_t(dec, &id);
//	vn_cs_handle_store_id((void **)val, id, VK_OBJECT_TYPE_*);
//
// We have no real object table, so we surface the stored id directly (the
// guest driver would translate it to a host pointer via the handle table).
func (d *Decoder) DecodeHandle() uint64 { return d.DecodeUint64() }
