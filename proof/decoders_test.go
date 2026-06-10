package proof

import (
	"bytes"
	"math"
	"testing"

	"github.com/go-virtio/venus/internal/vncs"
)

// These tests validate the generated reply/struct decoders against reply byte
// streams hand-derived from Mesa's venus-protocol decoders
// (vn_decode_vkEnumeratePhysicalDevices_reply in vn_protocol_driver_device.h,
// vn_decode_VkMemoryRequirements / vn_decode_VkPhysicalDeviceProperties in
// vn_protocol_driver_{structs,device}.h). Every expected stream is built
// field-by-field with le32/le64 in Mesa's decode order — none is produced by
// the encoder, so encode and decode are validated independently.

// ---- count + handle-array reply (vkEnumeratePhysicalDevices) ----

// TestDecodeEnumeratePhysicalDevicesReply crafts the reply stream by hand from
// vn_decode_vkEnumeratePhysicalDevices_reply:
//
//	vn_decode_VkCommandTypeEXT  -> int32 echo (2)
//	vn_decode_VkResult          -> int32
//	/* skip instance (an in-param, absent from the reply payload) */
//	if (vn_decode_simple_pointer) vn_decode_uint32_t(pCount)   // count present
//	if (vn_peek_array_size) { array_size(*pCount); N * VkPhysicalDevice }
//	else { array_size_unchecked /* consume the 0 */ }
func TestDecodeEnumeratePhysicalDevicesReply(t *testing.T) {
	// Present arm: count=2, two handle ids.
	var s []byte
	s = append(s, le32(uint32(VkEnumeratePhysicalDevicesReplyCmdType))...) // cmd echo = 2
	s = append(s, le32(0)...)                                              // VK_SUCCESS
	s = append(s, le64(1)...)                                              // simple_pointer(pCount) present
	s = append(s, le32(2)...)                                              // *pCount = 2
	s = append(s, le64(2)...)                                              // array_size(2)
	s = append(s, le64(0x111)...)                                          // VkPhysicalDevice[0]
	s = append(s, le64(0x222)...)                                          // VkPhysicalDevice[1]

	dec := vncs.NewDecoder(s)
	cmdType, result, count, countOK, devs := Decode_vkEnumeratePhysicalDevices_reply(dec)
	if cmdType != VkEnumeratePhysicalDevicesReplyCmdType {
		t.Fatalf("cmdType = %d want %d", cmdType, VkEnumeratePhysicalDevicesReplyCmdType)
	}
	if result != 0 || !countOK || count != 2 {
		t.Fatalf("result=%d countOK=%v count=%d", result, countOK, count)
	}
	if len(devs) != 2 || devs[0] != 0x111 || devs[1] != 0x222 {
		t.Fatalf("devs = %#x", devs)
	}
	if dec.Remaining() != 0 || dec.Fatal() {
		t.Fatalf("remaining=%d fatal=%v", dec.Remaining(), dec.Fatal())
	}
}

// TestDecodeEnumeratePhysicalDevicesReplyCountOnly exercises the count-query
// arm: pPhysicalDevices is NULL on the request, so the renderer replies with
// the count and an array_size(0) the decoder must still consume
// (vn_decode_array_size_unchecked), yielding a nil handle slice.
func TestDecodeEnumeratePhysicalDevicesReplyCountOnly(t *testing.T) {
	var s []byte
	s = append(s, le32(2)...) // cmd echo
	s = append(s, le32(5)...) // VK_INCOMPLETE-style nonzero result
	s = append(s, le64(1)...) // simple_pointer(pCount) present
	s = append(s, le32(8)...) // *pCount = 8 (host reports 8 devices available)
	s = append(s, le64(0)...) // array_size(0): no array requested
	dec := vncs.NewDecoder(s)
	cmdType, result, count, countOK, devs := Decode_vkEnumeratePhysicalDevices_reply(dec)
	if cmdType != 2 || result != 5 || !countOK || count != 8 {
		t.Fatalf("reply = (%d,%d,%v,%d)", cmdType, result, countOK, count)
	}
	if devs != nil {
		t.Fatalf("expected nil handle slice, got %#x", devs)
	}
	if dec.Remaining() != 0 || dec.Fatal() {
		t.Fatalf("remaining=%d fatal=%v", dec.Remaining(), dec.Fatal())
	}
}

// TestDecodeEnumeratePhysicalDevicesReplyNoCount exercises the absent-count
// arm (simple_pointer(0)), leaving countOK false; the array arm still runs.
func TestDecodeEnumeratePhysicalDevicesReplyNoCount(t *testing.T) {
	var s []byte
	s = append(s, le32(2)...) // cmd echo
	s = append(s, le32(0)...) // result
	s = append(s, le64(0)...) // simple_pointer(pCount) absent
	s = append(s, le64(0)...) // array_size(0)
	dec := vncs.NewDecoder(s)
	_, _, count, countOK, devs := Decode_vkEnumeratePhysicalDevices_reply(dec)
	if countOK || count != 0 || devs != nil {
		t.Fatalf("absent-count arm = (countOK=%v count=%d devs=%#x)", countOK, count, devs)
	}
}

// ---- returned-only struct decode (VkMemoryRequirements) ----

// TestDecodeVkMemoryRequirements hand-derives the stream from
// vn_decode_VkMemoryRequirements: VkDeviceSize size + VkDeviceSize alignment +
// uint32 memoryTypeBits, in order.
func TestDecodeVkMemoryRequirements(t *testing.T) {
	var s []byte
	s = append(s, le64(0x4000)...) // size
	s = append(s, le64(0x100)...)  // alignment
	s = append(s, le32(0x7)...)    // memoryTypeBits

	var mr VkMemoryRequirements
	dec := vncs.NewDecoder(s)
	DecodeVkMemoryRequirements(dec, &mr)
	if mr.Size != 0x4000 || mr.Alignment != 0x100 || mr.MemoryTypeBits != 0x7 {
		t.Fatalf("VkMemoryRequirements = %+v", mr)
	}
	if dec.Remaining() != 0 || dec.Fatal() {
		t.Fatalf("remaining=%d fatal=%v", dec.Remaining(), dec.Fatal())
	}
}

// f32 builds the LE bytes of a float32, independent of the encoder.
func f32(v float32) []byte { return le32(math.Float32bits(v)) }

// TestDecodeVkPhysicalDeviceProperties hand-derives the stream from
// vn_decode_VkPhysicalDeviceProperties (and the nested Limits /
// SparseProperties decoders), field-by-field in Mesa's decode order. The
// curated VkPhysicalDeviceLimits in the proof registry is a representative
// 15-member slice covering every member SHAPE the real Limits uses (uint32,
// VkDeviceSize, fixed uint32[N], float, size_t, int32, VkFlags, VkBool32,
// fixed float[N]); the full 106-member Limits is mechanically identical and is
// left out only to keep the hand-derived byte stream auditable.
func TestDecodeVkPhysicalDeviceProperties(t *testing.T) {
	// deviceName: char[256] = "gpu0\0..." padded to 256 bytes (the wire carries
	// all 256, array_size(256) prefix).
	name := make([]byte, 256)
	copy(name, "gpu0")
	// pipelineCacheUUID: uint8[16].
	uuid := []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15}

	var s []byte
	// VkPhysicalDeviceProperties head:
	s = append(s, le32(0x00402000)...) // apiVersion
	s = append(s, le32(0x10)...)       // driverVersion
	s = append(s, le32(0x1002)...)     // vendorID
	s = append(s, le32(0x67df)...)     // deviceID
	s = append(s, le32(2)...)          // deviceType (VK_PHYSICAL_DEVICE_TYPE_DISCRETE_GPU = 2)
	s = append(s, le64(256)...)        // array_size(VK_MAX_PHYSICAL_DEVICE_NAME_SIZE)
	s = append(s, name...)             // deviceName blob (already 256, a multiple of 4)
	s = append(s, le64(16)...)         // array_size(VK_UUID_SIZE)
	s = append(s, uuid...)             // pipelineCacheUUID blob (16, a multiple of 4)
	// VkPhysicalDeviceLimits (curated slice), in declaration order:
	s = append(s, le32(16384)...)      // maxImageDimension1D
	s = append(s, le32(16384)...)      // maxImageDimension2D
	s = append(s, le32(2048)...)       // maxImageDimension3D
	s = append(s, le32(16384)...)      // maxImageDimensionCube
	s = append(s, le32(2048)...)       // maxImageArrayLayers
	s = append(s, le32(0x8000000)...)  // maxTexelBufferElements
	s = append(s, le64(0x400)...)      // bufferImageGranularity (VkDeviceSize)
	s = append(s, le64(3)...)          // array_size(3) for maxComputeWorkGroupCount
	s = append(s, le32(65535)...)      // [0]
	s = append(s, le32(65535)...)      // [1]
	s = append(s, le32(65535)...)      // [2]
	s = append(s, f32(2.0)...)         // maxSamplerLodBias
	s = append(s, le64(64)...)         // minMemoryMapAlignment (size_t -> uint64)
	s = append(s, le32(0xfffffff8)...) // minTexelOffset (int32 = -8)
	s = append(s, le32(0xf)...)        // framebufferColorSampleCounts (VkFlags)
	s = append(s, le32(1)...)          // timestampComputeAndGraphics (VkBool32 true)
	s = append(s, le64(2)...)          // array_size(2) for pointSizeRange
	s = append(s, f32(1.0)...)         // [0]
	s = append(s, f32(64.0)...)        // [1]
	s = append(s, le64(0x40)...)       // nonCoherentAtomSize (VkDeviceSize)
	// VkPhysicalDeviceSparseProperties (5 VkBool32):
	s = append(s, le32(1)...) // residencyStandard2DBlockShape
	s = append(s, le32(0)...) // residencyStandard2DMultisampleBlockShape
	s = append(s, le32(1)...) // residencyStandard3DBlockShape
	s = append(s, le32(0)...) // residencyAlignedMipSize
	s = append(s, le32(1)...) // residencyNonResidentStrict

	var p VkPhysicalDeviceProperties
	dec := vncs.NewDecoder(s)
	DecodeVkPhysicalDeviceProperties(dec, &p)
	if dec.Remaining() != 0 || dec.Fatal() {
		t.Fatalf("remaining=%d fatal=%v", dec.Remaining(), dec.Fatal())
	}
	if p.ApiVersion != 0x00402000 || p.DriverVersion != 0x10 || p.VendorID != 0x1002 || p.DeviceID != 0x67df {
		t.Fatalf("head = %#x/%#x/%#x/%#x", p.ApiVersion, p.DriverVersion, p.VendorID, p.DeviceID)
	}
	if p.DeviceType != 2 {
		t.Fatalf("deviceType = %d", p.DeviceType)
	}
	if !bytes.Equal([]byte(p.DeviceName), name) {
		t.Fatalf("deviceName mismatch")
	}
	if !bytes.Equal(p.PipelineCacheUUID, uuid) {
		t.Fatalf("uuid = % x", p.PipelineCacheUUID)
	}
	// Spot-check every Limits member shape.
	l := p.Limits
	if l.MaxImageDimension1D != 16384 || l.MaxTexelBufferElements != 0x8000000 {
		t.Fatalf("limits uint32 = %d/%d", l.MaxImageDimension1D, l.MaxTexelBufferElements)
	}
	if l.BufferImageGranularity != 0x400 || l.NonCoherentAtomSize != 0x40 {
		t.Fatalf("limits VkDeviceSize = %#x/%#x", l.BufferImageGranularity, l.NonCoherentAtomSize)
	}
	if l.MaxComputeWorkGroupCount != [3]uint32{65535, 65535, 65535} {
		t.Fatalf("limits uint32[3] = %v", l.MaxComputeWorkGroupCount)
	}
	if l.MaxSamplerLodBias != 2.0 || l.PointSizeRange != [2]float32{1.0, 64.0} {
		t.Fatalf("limits float = %v/%v", l.MaxSamplerLodBias, l.PointSizeRange)
	}
	if l.MinMemoryMapAlignment != 64 {
		t.Fatalf("limits size_t = %d", l.MinMemoryMapAlignment)
	}
	if l.MinTexelOffset != -8 {
		t.Fatalf("limits int32 = %d", l.MinTexelOffset)
	}
	if l.FramebufferColorSampleCounts != 0xf || !l.TimestampComputeAndGraphics {
		t.Fatalf("limits flags/bool = %#x/%v", l.FramebufferColorSampleCounts, l.TimestampComputeAndGraphics)
	}
	sp := p.SparseProperties
	if !sp.ResidencyStandard2DBlockShape || sp.ResidencyStandard2DMultisampleBlockShape ||
		!sp.ResidencyStandard3DBlockShape || sp.ResidencyAlignedMipSize || !sp.ResidencyNonResidentStrict {
		t.Fatalf("sparse = %+v", sp)
	}
}
