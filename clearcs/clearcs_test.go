package clearcs

import (
	"bytes"
	"testing"
)

// le32/le64 build little-endian scalars for the hand-derived expected streams.
func le32(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}
func le64(v uint64) []byte {
	return []byte{
		byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24),
		byte(v >> 32), byte(v >> 40), byte(v >> 48), byte(v >> 56),
	}
}

// The clearcs façade is a thin []byte wrapper over the proof encoders/decoders
// (which are themselves byte-verified in package proof). These tests confirm
// the wrappers (a) produce the exact bytes the proof encoders produce for a
// representative command, and (b) round-trip every reply decoder against a
// hand-built reply stream, so the façade adds no transcription error.

// TestEncodeBindImageMemoryBytes hand-derives vkBindImageMemory
// (vn_encode_vkBindImageMemory: cmd_type(29) + flags + VkDevice + VkImage +
// VkDeviceMemory + VkDeviceSize) to anchor the façade against the wire.
func TestEncodeBindImageMemoryBytes(t *testing.T) {
	got := EncodeBindImageMemory(0, 0xD0, 0x20, 0x30, 0x1000)
	want := bytes.Join([][]byte{
		le32(29), le32(0), le64(0xD0), le64(0x20), le64(0x30), le64(0x1000),
	}, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeBindImageMemory\n got % x\nwant % x", got, want)
	}
}

// TestEncodeCmdClearColorImageBytes hand-derives the actual clear command
// (vn_encode_vkCmdClearColorImage: cmd_type(119) + flags + VkCommandBuffer +
// VkImage + VkImageLayout(int32) + simple_pointer(pColor)+union float arm +
// rangeCount + array_size + one VkImageSubresourceRange) for a red clear.
func TestEncodeCmdClearColorImageBytes(t *testing.T) {
	color := &VkClearColorValue{Tag: 0, Float32: [4]float32{1, 0, 0, 1}}
	rng := VkImageSubresourceRange{AspectMask: 0x1, BaseMipLevel: 0, LevelCount: 1, BaseArrayLayer: 0, LayerCount: 1}
	got := EncodeCmdClearColorImage(0, 0x10, 0x20, 1 /*GENERAL*/, color, []VkImageSubresourceRange{rng})

	f1 := le32(0x3F800000) // 1.0f
	f0 := le32(0x00000000) // 0.0f
	want := bytes.Join([][]byte{
		le32(119), le32(0), le64(0x10), le64(0x20), le32(1),
		le64(1), // simple_pointer(pColor)
		// VkClearColorValue union (vn_encode_VkClearColorValue): Tag (u32) +
		// array_size(4) (u64) + the active arm's 4 floats.
		le32(0),        // Tag = float32 arm
		le64(4),        // array_size = 4
		f1, f0, f0, f1, // float32[4] = (1,0,0,1)
		le32(1), le64(1), // rangeCount + array_size(1)
		// VkImageSubresourceRange: aspectMask, baseMip, levelCount, baseLayer, layerCount
		le32(0x1), le32(0), le32(1), le32(0), le32(1),
	}, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeCmdClearColorImage\n got % x\nwant % x", got, want)
	}
}

// TestEncodeCreateDeviceTrailingFeatures pins the generator-omission fix: the
// fixed VkDeviceCreateInfo body MUST end with a simple_pointer(NULL) for
// pEnabledFeatures (8-byte LE zero) — without it the host fails to peek 8 bytes
// and rejects vkCreateDevice (proven live against lavapipe vkr). We hand-derive
// the whole command for a single-queue, no-layer, no-extension device.
func TestEncodeCreateDeviceTrailingFeatures(t *testing.T) {
	dci := &VkDeviceCreateInfo{
		QueueCreateInfoCount: 1,
		PQueueCreateInfos: []VkDeviceQueueCreateInfo{{
			QueueFamilyIndex: 0, QueueCount: 1, PQueuePriorities: []float32{1},
		}},
	}
	got := EncodeCreateDevice(1, 0x2000, dci, 0x1002)

	f1 := le32(0x3F800000) // 1.0f priority
	queue := bytes.Join([][]byte{
		le32(2),     // sType DEVICE_QUEUE_CREATE_INFO
		le64(0),     // pNext NULL
		le32(0),     // flags
		le32(0),     // queueFamilyIndex
		le32(1),     // queueCount
		le64(1), f1, // array_size(1) + priority[0]
	}, nil)
	devInfo := bytes.Join([][]byte{
		le32(3), // sType DEVICE_CREATE_INFO
		le64(0), // pNext NULL
		le32(0), // flags
		le32(1), // queueCreateInfoCount
		le64(1), // array_size(1)
		queue,   // the one VkDeviceQueueCreateInfo
		le32(0), // enabledLayerCount
		le64(0), // ppEnabledLayerNames array_size(0)
		le32(0), // enabledExtensionCount
		le64(0), // ppEnabledExtensionNames array_size(0)
		le64(0), // pEnabledFeatures simple_pointer(NULL)  <-- the fix
	}, nil)
	want := bytes.Join([][]byte{
		le32(11), le32(1), le64(0x2000), // cmd_type, flags, physicalDevice
		le64(1), devInfo, // simple_pointer(pCreateInfo) + body
		le64(0),               // pAllocator simple_pointer(NULL)
		le64(1), le64(0x1002), // simple_pointer(pDevice) + handle
	}, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeCreateDevice (fixed)\n got % x\nwant % x", got, want)
	}
	// The NULL-pCreateInfo arm: simple_pointer(0), no body.
	gotNil := EncodeCreateDevice(0, 0x2000, nil, 0)
	wantNil := bytes.Join([][]byte{le32(11), le32(0), le64(0x2000), le64(0), le64(0), le64(0)}, nil)
	if !bytes.Equal(gotNil, wantNil) {
		t.Fatalf("EncodeCreateDevice nil-info\n got % x\nwant % x", gotNil, wantNil)
	}
}

// TestEncodeCreateDeviceWithLayersAndExts exercises the layer-name and
// extension-name array arms of the fixed VkDeviceCreateInfo encoder (and the
// nil-pQueueCreateInfos / pDevice=0 arms), so every branch of the
// generator-omission fix is covered. The encoded names are strings:
// array_size(strlen+1) + NUL-terminated bytes padded to 4.
func TestEncodeCreateDeviceWithLayersAndExts(t *testing.T) {
	dci := &VkDeviceCreateInfo{
		EnabledLayerCount:       1,
		PpEnabledLayerNames:     []string{"L"}, // "L\0\0\0", array_size 2
		EnabledExtensionCount:   1,
		PpEnabledExtensionNames: []string{"VK_x"}, // "VK_x\0\0\0\0", array_size 5
	}
	got := EncodeCreateDevice(0, 0x2000, dci, 0 /*pDevice=0 -> absent*/)

	devInfo := bytes.Join([][]byte{
		le32(3), le64(0), le32(0), // sType, pNext, flags
		le32(0), le64(0), // queueCreateInfoCount=0 + array_size(0) (nil pQueueCreateInfos)
		le32(1),                       // enabledLayerCount
		le64(1),                       // ppEnabledLayerNames array_size(1)
		le64(2), []byte{'L', 0, 0, 0}, // string "L": array_size(2)+padded bytes
		le32(1),                                         // enabledExtensionCount
		le64(1),                                         // array_size(1)
		le64(5), []byte{'V', 'K', '_', 'x', 0, 0, 0, 0}, // "VK_x": array_size(5)+padded
		le64(0), // pEnabledFeatures simple_pointer(NULL)
	}, nil)
	want := bytes.Join([][]byte{
		le32(11), le32(0), le64(0x2000),
		le64(1), devInfo,
		le64(0), // pAllocator NULL
		le64(0), // pDevice absent (pDevice==0)
	}, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeCreateDevice w/ layers+exts\n got % x\nwant % x", got, want)
	}
}

// TestEncodeCreateFence hand-derives vkCreateFence from Mesa
// vn_encode_vkCreateFence + vn_encode_VkFenceCreateInfo_self
// (vn_protocol_driver_fence.h): cmd_type(35) + flags + VkDevice +
// simple_pointer(pCreateInfo)+{sType(8)+pNext(0)+flags} +
// simple_pointer(pAllocator=0) + simple_pointer(pFence)+handle.
func TestEncodeCreateFence(t *testing.T) {
	got := EncodeCreateFence(1, 0xD0, &VkFenceCreateInfo{Flags: 0}, 0x1008)
	want := bytes.Join([][]byte{
		le32(35), le32(1), le64(0xD0),
		le64(1),          // simple_pointer(pCreateInfo)
		le32(8), le64(0), // sType FENCE_CREATE_INFO + pNext NULL
		le32(0),               // flags
		le64(0),               // pAllocator NULL
		le64(1), le64(0x1008), // simple_pointer(pFence) + handle
	}, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeCreateFence\n got % x\nwant % x", got, want)
	}
	// NULL-pCreateInfo + pFence==0 arms.
	gotNil := EncodeCreateFence(0, 0xD0, nil, 0)
	wantNil := bytes.Join([][]byte{le32(35), le32(0), le64(0xD0), le64(0), le64(0), le64(0)}, nil)
	if !bytes.Equal(gotNil, wantNil) {
		t.Fatalf("EncodeCreateFence nil\n got % x\nwant % x", gotNil, wantNil)
	}
}

// TestEncodeWaitForFences hand-derives vkWaitForFences (vn_encode_vkWaitForFences):
// cmd_type(39) + flags + VkDevice + fenceCount + array_size + VkFence + waitAll +
// timeout.
func TestEncodeWaitForFences(t *testing.T) {
	got := EncodeWaitForFences(1, 0xD0, []uint64{0xF1}, true, 0xFFFFFFFFFFFFFFFF)
	want := bytes.Join([][]byte{
		le32(39), le32(1), le64(0xD0),
		le32(1), le64(1), le64(0xF1), // fenceCount + array_size(1) + fence
		le32(1),                  // waitAll = true
		le64(0xFFFFFFFFFFFFFFFF), // timeout
	}, nil)
	if !bytes.Equal(got, want) {
		t.Fatalf("EncodeWaitForFences\n got % x\nwant % x", got, want)
	}
}

// TestEncodersNonEmpty exercises every façade encoder so a transcription typo
// (wrong proof function, swapped args) surfaces as a build/panic/empty-stream
// failure rather than only at runtime on the guest.
func TestEncodersNonEmpty(t *testing.T) {
	checks := map[string][]byte{
		"CreateInstance":          EncodeCreateInstance(1, &VkInstanceCreateInfo{PApplicationInfo: &VkApplicationInfo{}}, 0x1),
		"EnumPhysDevicesCount":    EncodeEnumeratePhysicalDevices(1, 0x1, 0, nil),
		"EnumPhysDevicesFetch":    EncodeEnumeratePhysicalDevices(1, 0x1, 1, make([]uint64, 1)),
		"MemoryProperties":        EncodeGetPhysicalDeviceMemoryProperties(1, 0x1),
		"QueueFamilyProperties":   EncodeGetPhysicalDeviceQueueFamilyProperties(1, 0x1, 0, nil),
		"CreateDevice":            EncodeCreateDevice(1, 0x1, &VkDeviceCreateInfo{QueueCreateInfoCount: 1, PQueueCreateInfos: []VkDeviceQueueCreateInfo{{QueueCount: 1, PQueuePriorities: []float32{1}}}}, 0x2),
		"GetDeviceQueue":          EncodeGetDeviceQueue(1, 0x2, 0, 0, 0x7),
		"CreateImage":             EncodeCreateImage(1, 0x2, &VkImageCreateInfo{ImageType: 1, Format: 37, Extent: VkExtent3D{16, 16, 1}, MipLevels: 1, ArrayLayers: 1, Samples: 1, Tiling: 1, Usage: 2}, 0x3),
		"ImageMemoryRequirements": EncodeGetImageMemoryRequirements(1, 0x2, 0x3),
		"AllocateMemory":          EncodeAllocateMemory(1, 0x2, &VkMemoryAllocateInfo{AllocationSize: 0x1000, MemoryTypeIndex: 0}, 0x4),
		"BindImageMemory":         EncodeBindImageMemory(1, 0x2, 0x3, 0x4, 0),
		"CreateCommandPool":       EncodeCreateCommandPool(1, 0x2, &VkCommandPoolCreateInfo{}, 0x5),
		"AllocateCommandBuffers":  EncodeAllocateCommandBuffers(1, 0x2, &VkCommandBufferAllocateInfo{CommandPool: 0x5, CommandBufferCount: 1}, []uint64{0x6}),
		"BeginCommandBuffer":      EncodeBeginCommandBuffer(1, 0x6, &VkCommandBufferBeginInfo{Flags: 1}),
		"EndCommandBuffer":        EncodeEndCommandBuffer(1, 0x6),
		"CmdPipelineBarrier":      EncodeCmdPipelineBarrier(0, 0x6, 1, 0x1000, 0, []VkImageMemoryBarrier{{Image: 0x3, SubresourceRange: VkImageSubresourceRange{AspectMask: 1, LevelCount: 1, LayerCount: 1}}}),
		"QueueSubmit":             EncodeQueueSubmit(1, 0x7, []VkSubmitInfo{{CommandBufferCount: 1, PCommandBuffers: []uint64{0x6}}}, 0),
		"QueueWaitIdle":           EncodeQueueWaitIdle(1, 0x7),
	}
	for name, b := range checks {
		if len(b) == 0 || len(b)%4 != 0 {
			t.Fatalf("%s produced len=%d (want non-zero, dword-aligned)", name, len(b))
		}
	}
}

// TestDecodersRoundTrip builds a hand-derived reply stream for each decoder and
// confirms the façade returns the decoded handle/result/struct unchanged.
func TestDecodersRoundTrip(t *testing.T) {
	// CreateInstance reply: cmd(0) + VkResult(0) + simple_pointer(1) + handle.
	if result, inst, ok := DecodeCreateInstanceReply(bytes.Join([][]byte{le32(0), le32(0), le64(1), le64(0xABC)}, nil)); result != 0 || inst != 0xABC || !ok {
		t.Fatalf("CreateInstance reply = (%d,%#x,%v)", result, inst, ok)
	}
	// EnumeratePhysicalDevices reply: cmd(2) + VkResult(0) + sp(pCount)+count +
	// array_size(1) + handle.
	if result, count, cok, devs := DecodeEnumeratePhysicalDevicesReply(bytes.Join([][]byte{le32(2), le32(0), le64(1), le32(1), le64(1), le64(0xD)}, nil)); result != 0 || count != 1 || !cok || len(devs) != 1 || devs[0] != 0xD {
		t.Fatalf("EnumPhysDevices reply = (%d,%d,%v,%v)", result, count, cok, devs)
	}
	// MemoryProperties reply: cmd(8) + sp(1) + typeCount + array_size + type{flags,heap} + heapCount + array_size + heap{size,flags}.
	mpReply := bytes.Join([][]byte{le32(8), le64(1), le32(1), le64(1), le32(0x6), le32(0), le32(1), le64(1), le64(0x100), le32(0x1)}, nil)
	if mp, ok := DecodeGetPhysicalDeviceMemoryPropertiesReply(mpReply); !ok || mp.MemoryTypeCount != 1 || mp.MemoryTypes[0].PropertyFlags != 0x6 {
		t.Fatalf("MemoryProperties reply = (%+v,%v)", mp, ok)
	}
	// QueueFamilyProperties reply: cmd(7) + sp(pCount)+count + array_size + one fam.
	qfReply := bytes.Join([][]byte{le32(7), le64(1), le32(1), le64(1), le32(0x7), le32(1), le32(0), le32(1), le32(1), le32(1)}, nil)
	if count, cok, fams := DecodeGetPhysicalDeviceQueueFamilyPropertiesReply(qfReply); !cok || count != 1 || len(fams) != 1 || fams[0].QueueFlags != 0x7 {
		t.Fatalf("QueueFamily reply = (%d,%v,%+v)", count, cok, fams)
	}
	// CreateDevice reply: cmd(11) + VkResult(0) + sp(1) + handle.
	if result, dev, ok := DecodeCreateDeviceReply(bytes.Join([][]byte{le32(11), le32(0), le64(1), le64(0xDE)}, nil)); result != 0 || dev != 0xDE || !ok {
		t.Fatalf("CreateDevice reply = (%d,%#x,%v)", result, dev, ok)
	}
	// GetDeviceQueue reply: cmd(17) + sp(1) + handle (no VkResult).
	if q, ok := DecodeGetDeviceQueueReply(bytes.Join([][]byte{le32(17), le64(1), le64(0xABBA)}, nil)); !ok || q != 0xABBA {
		t.Fatalf("GetDeviceQueue reply = (%#x,%v)", q, ok)
	}
	// CreateImage reply: cmd(54) + VkResult(0) + sp(1) + handle.
	if result, img, ok := DecodeCreateImageReply(bytes.Join([][]byte{le32(54), le32(0), le64(1), le64(0x1)}, nil)); result != 0 || img != 0x1 || !ok {
		t.Fatalf("CreateImage reply = (%d,%#x,%v)", result, img, ok)
	}
	// GetImageMemoryRequirements reply: cmd(31) + sp(1) + size + align + bits.
	if req, ok := DecodeGetImageMemoryRequirementsReply(bytes.Join([][]byte{le32(31), le64(1), le64(0x4000), le64(0x100), le32(0x7)}, nil)); !ok || req.Size != 0x4000 || req.MemoryTypeBits != 0x7 {
		t.Fatalf("ImageMemReq reply = (%+v,%v)", req, ok)
	}
	// AllocateMemory reply: cmd(21) + VkResult(0) + sp(1) + handle.
	if result, mem, ok := DecodeAllocateMemoryReply(bytes.Join([][]byte{le32(21), le32(0), le64(1), le64(0x40)}, nil)); result != 0 || mem != 0x40 || !ok {
		t.Fatalf("AllocateMemory reply = (%d,%#x,%v)", result, mem, ok)
	}
	// BindImageMemory reply: cmd(29) + VkResult(0).
	if r := DecodeBindImageMemoryReply(bytes.Join([][]byte{le32(29), le32(0)}, nil)); r != 0 {
		t.Fatalf("BindImageMemory reply = %d", r)
	}
	// CreateCommandPool reply: cmd(85) + VkResult(0) + sp(1) + handle.
	if result, pool, ok := DecodeCreateCommandPoolReply(bytes.Join([][]byte{le32(85), le32(0), le64(1), le64(0x50)}, nil)); result != 0 || pool != 0x50 || !ok {
		t.Fatalf("CreateCommandPool reply = (%d,%#x,%v)", result, pool, ok)
	}
	// AllocateCommandBuffers reply: cmd(88) + VkResult(0) + array_size(1) + handle.
	if result, bufs := DecodeAllocateCommandBuffersReply(bytes.Join([][]byte{le32(88), le32(0), le64(1), le64(0x60)}, nil), 1); result != 0 || len(bufs) != 1 || bufs[0] != 0x60 {
		t.Fatalf("AllocateCommandBuffers reply = (%d,%v)", result, bufs)
	}
	// Begin/End/QueueWaitIdle: cmd echo + VkResult(0).
	if r := DecodeBeginCommandBufferReply(bytes.Join([][]byte{le32(90), le32(0)}, nil)); r != 0 {
		t.Fatalf("Begin reply = %d", r)
	}
	if r := DecodeEndCommandBufferReply(bytes.Join([][]byte{le32(91), le32(0)}, nil)); r != 0 {
		t.Fatalf("End reply = %d", r)
	}
	if r := DecodeQueueWaitIdleReply(bytes.Join([][]byte{le32(19), le32(0)}, nil)); r != 0 {
		t.Fatalf("QueueWaitIdle reply = %d", r)
	}
	if r := DecodeWaitForFencesReply(bytes.Join([][]byte{le32(39), le32(0)}, nil)); r != 0 {
		t.Fatalf("WaitForFences reply = %d", r)
	}
	// CreateFence reply: cmd(35) + VkResult(0) + sp(1) + handle.
	if result, fence, ok := DecodeCreateFenceReply(bytes.Join([][]byte{le32(35), le32(0), le64(1), le64(0xF1)}, nil)); result != 0 || fence != 0xF1 || !ok {
		t.Fatalf("CreateFence reply = (%d,%#x,%v)", result, fence, ok)
	}
	// CreateFence wrong cmd_type -> not ok; absent pFence arm -> not ok.
	if _, _, ok := DecodeCreateFenceReply(bytes.Join([][]byte{le32(999), le32(0), le64(1), le64(0xF1)}, nil)); ok {
		t.Fatalf("CreateFence wrong cmd_type should not be ok")
	}
	if _, _, ok := DecodeCreateFenceReply(bytes.Join([][]byte{le32(35), le32(0), le64(0)}, nil)); ok {
		t.Fatalf("CreateFence absent-pFence should not be ok")
	}
	// QueueSubmit reply: cmd(18) + VkResult(0); wrong cmd_type flags the slot.
	if r, ok := DecodeQueueSubmitReply(bytes.Join([][]byte{le32(18), le32(0)}, nil)); r != 0 || !ok {
		t.Fatalf("QueueSubmit reply = (%d,%v)", r, ok)
	}
	if _, ok := DecodeQueueSubmitReply(bytes.Join([][]byte{le32(999), le32(0)}, nil)); ok {
		t.Fatalf("QueueSubmit reply with wrong cmd_type should not be ok")
	}
}
