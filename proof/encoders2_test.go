package proof

import (
	"bytes"
	"testing"

	"github.com/go-virtio/venus/internal/vncs"
)

// These tests extend encoders_test.go to the clear-image struct/command set
// and the reply decoders. Expected bytes are hand-derived from Mesa's
// venus-protocol headers (vn_protocol_driver_{structs,device,command_buffer,
// instance}.h), built field-by-field with the le32/le64/str helpers from
// encoders_test.go in Mesa field order. None reference the generated code.

// ---- nested-by-value structs ----

// TestEncodeVkExtent3D: Mesa vn_encode_VkExtent3D = three uint32 in order,
// no sType, no pNext.
func TestEncodeVkExtent3D(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkExtent3D(e, &VkExtent3D{Width: 64, Height: 32, Depth: 1})
	want := bytes.Join([][]byte{le32(64), le32(32), le32(1)}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkExtent3D\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkImageSubresourceRange: Mesa vn_encode_VkImageSubresourceRange =
// VkFlags aspectMask + 4 uint32, in order.
func TestEncodeVkImageSubresourceRange(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkImageSubresourceRange(e, &VkImageSubresourceRange{
		AspectMask: 0x1, BaseMipLevel: 0, LevelCount: 1, BaseArrayLayer: 0, LayerCount: 1,
	})
	want := bytes.Join([][]byte{le32(0x1), le32(0), le32(1), le32(0), le32(1)}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkImageSubresourceRange\n got % x\nwant % x", e.Bytes(), want)
	}
}

// ---- the VkClearColorValue union (all three arms) ----

// TestEncodeVkClearColorValue: Mesa vn_encode_VkClearColorValue_tag =
// uint32 tag + array_size(4) + the selected 4-element arm.
func TestEncodeVkClearColorValue(t *testing.T) {
	// tag 2 (uint32 arm) is Mesa's default (vn_encode_VkClearColorValue).
	eU := vncs.NewEncoder()
	EncodeVkClearColorValue(eU, &VkClearColorValue{Tag: 2, Uint32: [4]uint32{1, 2, 3, 4}})
	wantU := bytes.Join([][]byte{le32(2), le64(4), le32(1), le32(2), le32(3), le32(4)}, nil)
	if !bytes.Equal(eU.Bytes(), wantU) {
		t.Fatalf("union uint32 arm\n got % x\nwant % x", eU.Bytes(), wantU)
	}

	// tag 0 (float32 arm): 1.0,0,0,1.0.
	eF := vncs.NewEncoder()
	EncodeVkClearColorValue(eF, &VkClearColorValue{Tag: 0, Float32: [4]float32{1, 0, 0, 1}})
	wantF := bytes.Join([][]byte{le32(0), le64(4), le32(0x3f800000), le32(0), le32(0), le32(0x3f800000)}, nil)
	if !bytes.Equal(eF.Bytes(), wantF) {
		t.Fatalf("union float arm\n got % x\nwant % x", eF.Bytes(), wantF)
	}

	// tag 1 (int32 arm): -1,0,0,0.
	eI := vncs.NewEncoder()
	EncodeVkClearColorValue(eI, &VkClearColorValue{Tag: 1, Int32: [4]int32{-1, 0, 0, 0}})
	wantI := bytes.Join([][]byte{le32(1), le64(4), le32(0xffffffff), le32(0), le32(0), le32(0)}, nil)
	if !bytes.Equal(eI.Bytes(), wantI) {
		t.Fatalf("union int32 arm\n got % x\nwant % x", eI.Bytes(), wantI)
	}
}

// ---- sType struct with VkDeviceSize + enum + nested-by-value ----

// TestEncodeVkMemoryAllocateInfo: sType(5) + pNext NULL + VkDeviceSize
// (uint64) + uint32.
func TestEncodeVkMemoryAllocateInfo(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkMemoryAllocateInfo(e, &VkMemoryAllocateInfo{AllocationSize: 0x1000, MemoryTypeIndex: 3})
	want := bytes.Join([][]byte{le32(5), le64(0), le64(0x1000), le32(3)}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkMemoryAllocateInfo\n got % x\nwant % x", e.Bytes(), want)
	}
}

// imageCreateInfoExpected hand-derives the bytes for the sample below from
// Mesa vn_encode_VkImageCreateInfo / _self: sType(14) + pNext NULL + flags +
// imageType(enum int32) + format(enum int32) + nested VkExtent3D (3 uint32) +
// mipLevels + arrayLayers + samples(VkFlags) + tiling(enum) + usage(VkFlags) +
// sharingMode(enum) + queueFamilyIndexCount + (pQueueFamilyIndices NULL ->
// array_size(0)) + initialLayout(enum).
func imageCreateInfoExpected(ci *VkImageCreateInfo) []byte {
	var w []byte
	w = append(w, le32(14)...) // sType IMAGE_CREATE_INFO
	w = append(w, le64(0)...)  // pNext NULL
	w = append(w, le32(ci.Flags)...)
	w = append(w, le32(uint32(ci.ImageType))...)
	w = append(w, le32(uint32(ci.Format))...)
	w = append(w, le32(ci.Extent.Width)...)
	w = append(w, le32(ci.Extent.Height)...)
	w = append(w, le32(ci.Extent.Depth)...)
	w = append(w, le32(ci.MipLevels)...)
	w = append(w, le32(ci.ArrayLayers)...)
	w = append(w, le32(ci.Samples)...)
	w = append(w, le32(uint32(ci.Tiling))...)
	w = append(w, le32(ci.Usage)...)
	w = append(w, le32(uint32(ci.SharingMode))...)
	w = append(w, le32(ci.QueueFamilyIndexCount)...)
	if len(ci.PQueueFamilyIndices) != 0 {
		w = append(w, le64(uint64(len(ci.PQueueFamilyIndices)))...)
		for _, v := range ci.PQueueFamilyIndices {
			w = append(w, le32(v)...)
		}
	} else {
		w = append(w, le64(0)...)
	}
	w = append(w, le32(uint32(ci.InitialLayout))...)
	return w
}

func sampleImageCreateInfo() *VkImageCreateInfo {
	return &VkImageCreateInfo{
		Flags:                 0,
		ImageType:             1,  // VK_IMAGE_TYPE_2D
		Format:                37, // VK_FORMAT_R8G8B8A8_UNORM
		Extent:                VkExtent3D{Width: 64, Height: 64, Depth: 1},
		MipLevels:             1,
		ArrayLayers:           1,
		Samples:               1,   // VK_SAMPLE_COUNT_1_BIT
		Tiling:                0,   // VK_IMAGE_TILING_OPTIMAL
		Usage:                 0x9, // TRANSFER_DST | SAMPLED-ish flag bits
		SharingMode:           0,   // VK_SHARING_MODE_EXCLUSIVE
		QueueFamilyIndexCount: 0,
		PQueueFamilyIndices:   nil,
		InitialLayout:         0, // VK_IMAGE_LAYOUT_UNDEFINED
	}
}

func TestEncodeVkImageCreateInfo(t *testing.T) {
	ci := sampleImageCreateInfo()
	e := vncs.NewEncoder()
	EncodeVkImageCreateInfo(e, ci)
	if want := imageCreateInfoExpected(ci); !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkImageCreateInfo\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkImageCreateInfoSharedQueues exercises the present-array branch
// of pQueueFamilyIndices (counted uint32 array inside a struct).
func TestEncodeVkImageCreateInfoSharedQueues(t *testing.T) {
	ci := sampleImageCreateInfo()
	ci.SharingMode = 1 // VK_SHARING_MODE_CONCURRENT
	ci.QueueFamilyIndexCount = 2
	ci.PQueueFamilyIndices = []uint32{0, 1}
	e := vncs.NewEncoder()
	EncodeVkImageCreateInfo(e, ci)
	if want := imageCreateInfoExpected(ci); !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkImageCreateInfo shared\n got % x\nwant % x", e.Bytes(), want)
	}
}

// ---- VkDeviceCreateInfo + vkCreateDevice (REQUIRED byte validation) ----

// deviceCreateInfoExpected hand-derives the bytes for a VkDeviceCreateInfo
// from Mesa vn_encode_VkDeviceCreateInfo / _self: sType(3) + pNext NULL +
// flags + queueCreateInfoCount + (pQueueCreateInfos present: array_size(N) +
// each VkDeviceQueueCreateInfo) + enabledLayerCount + layer names +
// enabledExtensionCount + ext names. pEnabledFeatures is not in our subset.
func deviceQueueCreateInfoExpected(q *VkDeviceQueueCreateInfo) []byte {
	var w []byte
	w = append(w, le32(2)...) // sType DEVICE_QUEUE_CREATE_INFO
	w = append(w, le64(0)...) // pNext NULL
	w = append(w, le32(q.Flags)...)
	w = append(w, le32(q.QueueFamilyIndex)...)
	w = append(w, le32(q.QueueCount)...)
	if len(q.PQueuePriorities) != 0 {
		w = append(w, le64(uint64(len(q.PQueuePriorities)))...)
		for _, p := range q.PQueuePriorities {
			w = append(w, le32(float32bits(p))...)
		}
	} else {
		w = append(w, le64(0)...)
	}
	return w
}

func deviceCreateInfoExpected(dc *VkDeviceCreateInfo) []byte {
	var w []byte
	w = append(w, le32(3)...) // sType DEVICE_CREATE_INFO
	w = append(w, le64(0)...) // pNext NULL
	w = append(w, le32(dc.Flags)...)
	w = append(w, le32(dc.QueueCreateInfoCount)...)
	if len(dc.PQueueCreateInfos) != 0 {
		w = append(w, le64(uint64(len(dc.PQueueCreateInfos)))...)
		for i := range dc.PQueueCreateInfos {
			w = append(w, deviceQueueCreateInfoExpected(&dc.PQueueCreateInfos[i])...)
		}
	} else {
		w = append(w, le64(0)...)
	}
	w = append(w, le32(dc.EnabledLayerCount)...)
	if len(dc.PpEnabledLayerNames) != 0 {
		w = append(w, le64(uint64(len(dc.PpEnabledLayerNames)))...)
		for _, s := range dc.PpEnabledLayerNames {
			w = append(w, str(s)...)
		}
	} else {
		w = append(w, le64(0)...)
	}
	w = append(w, le32(dc.EnabledExtensionCount)...)
	if len(dc.PpEnabledExtensionNames) != 0 {
		w = append(w, le64(uint64(len(dc.PpEnabledExtensionNames)))...)
		for _, s := range dc.PpEnabledExtensionNames {
			w = append(w, str(s)...)
		}
	} else {
		w = append(w, le64(0)...)
	}
	return w
}

func sampleDeviceCreateInfo() *VkDeviceCreateInfo {
	return &VkDeviceCreateInfo{
		Flags:                0,
		QueueCreateInfoCount: 1,
		PQueueCreateInfos: []VkDeviceQueueCreateInfo{{
			Flags:            0,
			QueueFamilyIndex: 0,
			QueueCount:       1,
			PQueuePriorities: []float32{1.0},
		}},
		EnabledLayerCount:       0,
		PpEnabledLayerNames:     nil,
		EnabledExtensionCount:   1,
		PpEnabledExtensionNames: []string{"VK_KHR_swapchain"},
	}
}

func TestEncodeVkDeviceCreateInfo(t *testing.T) {
	dc := sampleDeviceCreateInfo()
	e := vncs.NewEncoder()
	EncodeVkDeviceCreateInfo(e, dc)
	if want := deviceCreateInfoExpected(dc); !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkDeviceCreateInfo\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkDeviceCreateInfoEmptyQueues exercises the empty-array branch of
// pQueueCreateInfos and pQueuePriorities (array_size(0)).
func TestEncodeVkDeviceCreateInfoEmptyQueues(t *testing.T) {
	dc := &VkDeviceCreateInfo{}
	e := vncs.NewEncoder()
	EncodeVkDeviceCreateInfo(e, dc)
	if want := deviceCreateInfoExpected(dc); !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("empty-queue VkDeviceCreateInfo\n got % x\nwant % x", e.Bytes(), want)
	}
	// And the empty pQueuePriorities arm directly.
	eq := vncs.NewEncoder()
	q := &VkDeviceQueueCreateInfo{QueueCount: 0}
	EncodeVkDeviceQueueCreateInfo(eq, q)
	if want := deviceQueueCreateInfoExpected(q); !bytes.Equal(eq.Bytes(), want) {
		t.Fatalf("empty-prio queue\n got % x\nwant % x", eq.Bytes(), want)
	}
}

// TestEncodeVkCreateDevice is the REQUIRED command-encode byte validation.
// Mesa vn_encode_vkCreateDevice (vn_protocol_driver_device.h):
//
//	cmd_type(int32 = 11) + cmd_flags(VkFlags) +
//	vn_encode_VkPhysicalDevice(physicalDevice)             // by-value handle
//	if (simple_pointer(pCreateInfo)) vn_encode_VkDeviceCreateInfo(...)
//	if (simple_pointer(pAllocator)) assert(false)          // NULL
//	if (simple_pointer(pDevice)) vn_encode_VkDevice(pDevice)
func TestEncodeVkCreateDevice(t *testing.T) {
	dc := sampleDeviceCreateInfo()
	const physID = uint64(0xABCD)
	const pDevice = uint64(0) // NULL out-handle

	e := vncs.NewEncoder()
	Encode_vkCreateDevice(e, 0, physID, dc, pDevice)

	var want []byte
	want = append(want, le32(11)...)     // cmd_type vkCreateDevice = 11
	want = append(want, le32(0)...)      // cmd_flags
	want = append(want, le64(physID)...) // physicalDevice (by-value handle id)
	want = append(want, le64(1)...)      // simple_pointer(pCreateInfo) present
	want = append(want, deviceCreateInfoExpected(dc)...)
	want = append(want, le64(0)...) // simple_pointer(pAllocator) NULL
	want = append(want, le64(0)...) // simple_pointer(pDevice): id 0 -> absent

	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkCreateDevice\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkCreateDevicePresentHandle exercises the present-out-handle arm.
func TestEncodeVkCreateDevicePresentHandle(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkCreateDevice(e, 0, 0xABCD, &VkDeviceCreateInfo{}, 0x55)
	var want []byte
	want = append(want, le32(11)...)
	want = append(want, le32(0)...)
	want = append(want, le64(0xABCD)...)
	want = append(want, le64(1)...)
	want = append(want, deviceCreateInfoExpected(&VkDeviceCreateInfo{})...)
	want = append(want, le64(0)...)    // pAllocator
	want = append(want, le64(1)...)    // pDevice present
	want = append(want, le64(0x55)...) // pDevice handle id
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkCreateDevice present\n got % x\nwant % x", e.Bytes(), want)
	}
}

// ---- vkCmdClearColorImage (the union + handles + enum + struct array) ----

func TestEncodeVkCmdClearColorImage(t *testing.T) {
	color := &VkClearColorValue{Tag: 0, Float32: [4]float32{0, 0, 1, 1}}
	ranges := []VkImageSubresourceRange{{AspectMask: 0x1, LevelCount: 1, LayerCount: 1}}

	e := vncs.NewEncoder()
	Encode_vkCmdClearColorImage(e, 0, 0x10 /*cmdbuf*/, 0x20 /*image*/, 1 /*GENERAL*/, color, 1, ranges)

	var want []byte
	want = append(want, le32(119)...)  // cmd_type vkCmdClearColorImage = 119
	want = append(want, le32(0)...)    // cmd_flags
	want = append(want, le64(0x10)...) // commandBuffer (by-value handle)
	want = append(want, le64(0x20)...) // image (by-value handle)
	want = append(want, le32(1)...)    // imageLayout enum (VK_IMAGE_LAYOUT_GENERAL=1)
	want = append(want, le64(1)...)    // simple_pointer(pColor) present
	// union float arm: tag 0 + array_size(4) + 4 floats
	want = append(want, bytes.Join([][]byte{le32(0), le64(4)}, nil)...)
	want = append(want, bytes.Join([][]byte{le32(0), le32(0), le32(0x3f800000), le32(0x3f800000)}, nil)...)
	want = append(want, le32(1)...) // rangeCount
	want = append(want, le64(1)...) // array_size(1)
	want = append(want, bytes.Join([][]byte{le32(0x1), le32(0), le32(1), le32(0), le32(1)}, nil)...)

	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkCmdClearColorImage\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkCmdClearColorImageEmptyRanges exercises the NULL pColor arm and
// the empty-ranges (array_size(0)) arm.
func TestEncodeVkCmdClearColorImageEmptyRanges(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkCmdClearColorImage(e, 0, 0x10, 0x20, 1, nil, 0, nil)
	var want []byte
	want = append(want, le32(119)...)
	want = append(want, le32(0)...)
	want = append(want, le64(0x10)...)
	want = append(want, le64(0x20)...)
	want = append(want, le32(1)...)
	want = append(want, le64(0)...) // simple_pointer(pColor) NULL
	want = append(want, le32(0)...) // rangeCount
	want = append(want, le64(0)...) // array_size(0)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("clear empty\n got % x\nwant % x", e.Bytes(), want)
	}
}

// ---- vkEnumeratePhysicalDevices encode (handle by value + out count + array) ----

func TestEncodeVkEnumeratePhysicalDevices(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkEnumeratePhysicalDevices(e, 0, 0x01 /*instance*/, 4 /*count*/, []uint64{0x7, 0x8})
	var want []byte
	want = append(want, le32(2)...)    // cmd_type = 2
	want = append(want, le32(0)...)    // cmd_flags
	want = append(want, le64(0x01)...) // instance handle
	want = append(want, le64(1)...)    // simple_pointer(pPhysicalDeviceCount) present
	want = append(want, le32(4)...)    // count value
	want = append(want, le64(2)...)    // array_size(2)
	want = append(want, bytes.Join([][]byte{le64(0x7), le64(0x8)}, nil)...)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("enumerate\n got % x\nwant % x", e.Bytes(), want)
	}
	// empty out-array arm.
	e2 := vncs.NewEncoder()
	Encode_vkEnumeratePhysicalDevices(e2, 0, 0x01, 0, nil)
	want2 := bytes.Join([][]byte{le32(2), le32(0), le64(0x01), le64(1), le32(0), le64(0)}, nil)
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("enumerate empty\n got % x\nwant % x", e2.Bytes(), want2)
	}
}

// ---- REPLY DECODE (REQUIRED) ----

// TestDecodeVkCreateInstanceReply is the REQUIRED reply-decode validation. It
// crafts a reply byte stream by hand from Mesa vn_decode_vkCreateInstance_reply
// (vn_protocol_driver_instance.h):
//
//	cmd_type echo (int32 = 0) + VkResult (int32) + simple_pointer(present) +
//	VkInstance handle id
//
// and asserts the decoded (cmdType, result, handle, ok).
func TestDecodeVkCreateInstanceReply(t *testing.T) {
	const (
		wantResult = int32(0) // VK_SUCCESS
		wantID     = uint64(0xDEADBEEFCAFE)
	)
	var stream []byte
	stream = append(stream, le32(0)...)                  // echoed cmd_type vkCreateInstance = 0
	stream = append(stream, le32(uint32(wantResult))...) // VkResult VK_SUCCESS
	stream = append(stream, le64(1)...)                  // simple_pointer present
	stream = append(stream, le64(wantID)...)             // VkInstance id

	dec := vncs.NewDecoder(stream)
	cmdType, result, instance, ok := Decode_vkCreateInstance_reply(dec)
	if cmdType != VkCreateInstanceReplyCmdType {
		t.Fatalf("cmdType = %d want %d", cmdType, VkCreateInstanceReplyCmdType)
	}
	if result != wantResult {
		t.Fatalf("result = %d want %d", result, wantResult)
	}
	if !ok || instance != wantID {
		t.Fatalf("instance = %#x ok=%v want %#x true", instance, ok, wantID)
	}
	if dec.Remaining() != 0 || dec.Fatal() {
		t.Fatalf("remaining=%d fatal=%v", dec.Remaining(), dec.Fatal())
	}
}

// TestDecodeVkCreateInstanceReplyError exercises an error result with the NULL
// out-handle arm (simple_pointer = 0).
func TestDecodeVkCreateInstanceReplyError(t *testing.T) {
	var stream []byte
	stream = append(stream, le32(0)...)          // cmd_type echo
	stream = append(stream, le32(0xFFFFFFFF)...) // VkResult = -1 (an error code)
	stream = append(stream, le64(0)...)          // simple_pointer absent
	dec := vncs.NewDecoder(stream)
	_, result, instance, ok := Decode_vkCreateInstance_reply(dec)
	if result != -1 {
		t.Fatalf("result = %d want -1", result)
	}
	if ok || instance != 0 {
		t.Fatalf("expected absent handle, got id=%#x ok=%v", instance, ok)
	}
}

// TestDecodeVkCreateDeviceReply validates the create-device reply decode
// (cmd_type 11) end-to-end against a crafted stream.
func TestDecodeVkCreateDeviceReply(t *testing.T) {
	const wantID = uint64(0x1234)
	var stream []byte
	stream = append(stream, le32(11)...) // echoed cmd_type vkCreateDevice = 11
	stream = append(stream, le32(0)...)  // VK_SUCCESS
	stream = append(stream, le64(1)...)  // simple_pointer present
	stream = append(stream, le64(wantID)...)
	dec := vncs.NewDecoder(stream)
	cmdType, result, device, ok := Decode_vkCreateDevice_reply(dec)
	if cmdType != VkCreateDeviceReplyCmdType || result != 0 || !ok || device != wantID {
		t.Fatalf("reply = (%d,%d,%#x,%v)", cmdType, result, device, ok)
	}
}

// TestRoundTripCreateImageReply confirms a create reply built by the same
// framing decodes back (covers the remaining reply decoders + handle path).
func TestRoundTripCreateImageReply(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cmd    int32
		decode func(*vncs.Decoder) (int32, int32, uint64, bool)
	}{
		{"image", VkCreateImageReplyCmdType, Decode_vkCreateImage_reply},
		{"memory", VkAllocateMemoryReplyCmdType, Decode_vkAllocateMemory_reply},
		{"pool", VkCreateCommandPoolReplyCmdType, Decode_vkCreateCommandPool_reply},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var s []byte
			s = append(s, le32(uint32(tc.cmd))...)
			s = append(s, le32(0)...)
			s = append(s, le64(1)...)
			s = append(s, le64(0x99)...)
			cmdType, result, h, ok := tc.decode(vncs.NewDecoder(s))
			if cmdType != tc.cmd || result != 0 || !ok || h != 0x99 {
				t.Fatalf("%s reply = (%d,%d,%#x,%v)", tc.name, cmdType, result, h, ok)
			}
		})
	}
}

// TestEncodeVkDeviceCreateInfoWithLayers exercises the enabled-layer-names
// present branch of VkDeviceCreateInfo (the 85.7% gap).
func TestEncodeVkDeviceCreateInfoWithLayers(t *testing.T) {
	dc := &VkDeviceCreateInfo{
		EnabledLayerCount:   1,
		PpEnabledLayerNames: []string{"VK_LAYER_KHRONOS_validation"},
	}
	e := vncs.NewEncoder()
	EncodeVkDeviceCreateInfo(e, dc)
	if want := deviceCreateInfoExpected(dc); !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("device with layers\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkCommandPoolCreateInfo: sType(39) + pNext NULL + flags + uint32.
func TestEncodeVkCommandPoolCreateInfo(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkCommandPoolCreateInfo(e, &VkCommandPoolCreateInfo{Flags: 0x2, QueueFamilyIndex: 0})
	want := bytes.Join([][]byte{le32(39), le64(0), le32(0x2), le32(0)}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkCommandPoolCreateInfo\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkCommandBufferAllocateInfo: sType(40) + pNext NULL + commandPool
// (by-value handle) + level(enum int32) + commandBufferCount(uint32).
func TestEncodeVkCommandBufferAllocateInfo(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkCommandBufferAllocateInfo(e, &VkCommandBufferAllocateInfo{
		CommandPool: 0xCAFE, Level: 0 /*PRIMARY*/, CommandBufferCount: 1,
	})
	want := bytes.Join([][]byte{le32(40), le64(0), le64(0xCAFE), le32(0), le32(1)}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkCommandBufferAllocateInfo\n got % x\nwant % x", e.Bytes(), want)
	}
}

// createCmdExpected builds the common create-command framing: cmd_type + flags
// + device(by-value handle) + simple_pointer(pCreateInfo)+struct + pAllocator
// NULL + simple_pointer(out-handle, absent for id 0).
func createCmdExpected(cmdType int32, device uint64, info []byte) []byte {
	var w []byte
	w = append(w, le32(uint32(cmdType))...)
	w = append(w, le32(0)...)      // cmd_flags
	w = append(w, le64(device)...) // device handle (by value)
	w = append(w, le64(1)...)      // simple_pointer(pCreateInfo) present
	w = append(w, info...)
	w = append(w, le64(0)...) // pAllocator NULL
	w = append(w, le64(0)...) // out-handle absent (id 0)
	return w
}

// createCmdExpectedH is createCmdExpected with a present out-handle id.
func createCmdExpectedH(cmdType int32, device uint64, info []byte, outID uint64) []byte {
	var w []byte
	w = append(w, le32(uint32(cmdType))...)
	w = append(w, le32(0)...)
	w = append(w, le64(device)...)
	w = append(w, le64(1)...)
	w = append(w, info...)
	w = append(w, le64(0)...)     // pAllocator NULL
	w = append(w, le64(1)...)     // out-handle present
	w = append(w, le64(outID)...) // out-handle id
	return w
}

func TestEncodeVkCreateImage(t *testing.T) {
	ci := sampleImageCreateInfo()
	e := vncs.NewEncoder()
	Encode_vkCreateImage(e, 0, 0xD0 /*device*/, ci, 0 /*pImage*/)
	want := createCmdExpected(54, 0xD0, imageCreateInfoExpected(ci))
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkCreateImage\n got % x\nwant % x", e.Bytes(), want)
	}
	// present out-handle arm.
	e2 := vncs.NewEncoder()
	Encode_vkCreateImage(e2, 0, 0xD0, ci, 0x77)
	if want := createCmdExpectedH(54, 0xD0, imageCreateInfoExpected(ci), 0x77); !bytes.Equal(e2.Bytes(), want) {
		t.Fatalf("vkCreateImage present\n got % x\nwant % x", e2.Bytes(), want)
	}
}

func TestEncodeVkAllocateMemory(t *testing.T) {
	mi := &VkMemoryAllocateInfo{AllocationSize: 0x2000, MemoryTypeIndex: 1}
	e := vncs.NewEncoder()
	Encode_vkAllocateMemory(e, 0, 0xD0, mi, 0)
	info := bytes.Join([][]byte{le32(5), le64(0), le64(0x2000), le32(1)}, nil)
	want := createCmdExpected(21, 0xD0, info)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkAllocateMemory\n got % x\nwant % x", e.Bytes(), want)
	}
	e2 := vncs.NewEncoder()
	Encode_vkAllocateMemory(e2, 0, 0xD0, mi, 0x88)
	if want := createCmdExpectedH(21, 0xD0, info, 0x88); !bytes.Equal(e2.Bytes(), want) {
		t.Fatalf("vkAllocateMemory present\n got % x\nwant % x", e2.Bytes(), want)
	}
}

func TestEncodeVkCreateCommandPool(t *testing.T) {
	pi := &VkCommandPoolCreateInfo{Flags: 0x2, QueueFamilyIndex: 0}
	e := vncs.NewEncoder()
	Encode_vkCreateCommandPool(e, 0, 0xD0, pi, 0)
	info := bytes.Join([][]byte{le32(39), le64(0), le32(0x2), le32(0)}, nil)
	want := createCmdExpected(85, 0xD0, info)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkCreateCommandPool\n got % x\nwant % x", e.Bytes(), want)
	}
	e2 := vncs.NewEncoder()
	Encode_vkCreateCommandPool(e2, 0, 0xD0, pi, 0x66)
	if want := createCmdExpectedH(85, 0xD0, info, 0x66); !bytes.Equal(e2.Bytes(), want) {
		t.Fatalf("vkCreateCommandPool present\n got % x\nwant % x", e2.Bytes(), want)
	}
}

// float32bits is a local helper mirroring math.Float32bits for the
// hand-derived float arrays, kept independent of the encoder.
func float32bits(f float32) uint32 {
	switch f {
	case 1.0:
		return 0x3f800000
	case 0.0:
		return 0x00000000
	}
	// Only the exact constants above are used by these tests.
	panic("float32bits: unexpected test constant")
}
