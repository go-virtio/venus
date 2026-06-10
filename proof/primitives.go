// Package proof holds the Venus M0 proof set: the generated Venus struct and
// command encoders (encoders_gen.go) plus thin named wrappers for the
// individual wire primitives, all validated byte-for-byte against
// Mesa-derived expected bytes in the package tests.
//
// The primitive wrappers below exist so the proof set names every wire rule
// the M0 charter calls out (uint32/int32/float32/uint64/VkFlags/handle/
// string/array_size) as a directly testable surface. They are deliberately
// 1:1 with the vncs runtime methods; the real encoding lives in vncs (with
// the Mesa citations).
package proof

import "github.com/go-virtio/venus/internal/vncs"

// EncodeUint32 emits a 4-byte LE uint32 (Mesa vn_encode_uint32_t).
func EncodeUint32(enc *vncs.Encoder, v uint32) { enc.EncodeUint32(v) }

// EncodeInt32 emits a 4-byte LE int32 (Mesa vn_encode_int32_t; also the
// path for VkStructureType / VkCommandTypeEXT).
func EncodeInt32(enc *vncs.Encoder, v int32) { enc.EncodeInt32(v) }

// EncodeUint64 emits an 8-byte LE uint64 (Mesa vn_encode_uint64_t).
func EncodeUint64(enc *vncs.Encoder, v uint64) { enc.EncodeUint64(v) }

// EncodeFloat32 emits a 4-byte LE IEEE-754 float (Mesa vn_encode_float).
func EncodeFloat32(enc *vncs.Encoder, v float32) { enc.EncodeFloat32(v) }

// EncodeFlags emits a 4-byte LE VkFlags (Mesa vn_encode_VkFlags ->
// vn_encode_uint32_t).
func EncodeFlags(enc *vncs.Encoder, v uint32) { enc.EncodeFlags(v) }

// EncodeHandle emits an 8-byte LE object id (Mesa vn_encode_VkInstance and
// peers: vn_encode_uint64_t of the handle id).
func EncodeHandle(enc *vncs.Encoder, id uint64) { enc.EncodeHandle(id) }

// EncodeArraySize emits an 8-byte LE array-size prefix (Mesa
// vn_encode_array_size -> vn_encode_uint64_t).
func EncodeArraySize(enc *vncs.Encoder, size uint64) { enc.EncodeArraySize(size) }

// EncodeString emits a Venus string: array_size(strlen+1) then the
// NUL-terminated bytes padded to 4 (Mesa vn_encode_array_size +
// vn_encode_char_array).
func EncodeString(enc *vncs.Encoder, s string) { enc.EncodeString(s) }
