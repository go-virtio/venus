package proof

import (
	"bytes"
	"testing"

	"github.com/go-virtio/venus/internal/vncs"
)

// These tests close the clear-image-to-readback command set: the device/queue
// setup, image memory binding, command-buffer recording, queue completion, and
// the physical-device property queries. Every expected byte stream is
// hand-derived from Mesa's venus-protocol headers
// (vn_protocol_driver_{device,image,command_buffer,queue,fence,structs}.h),
// built field-by-field with the le32/le64/str helpers in Mesa field order. None
// references the generated code.

// ---- vkGetDeviceQueue (encode + void single-handle reply) ----

// TestEncodeVkGetDeviceQueue derives the command from vn_encode_vkGetDeviceQueue
// (vn_protocol_driver_device.h): cmd_type(17) + cmd_flags + VkDevice(handle) +
// uint32 queueFamilyIndex + uint32 queueIndex + simple_pointer(pQueue) (absent
// when the out-handle id is 0).
func TestEncodeVkGetDeviceQueue(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkGetDeviceQueue(e, 0, 0xD0 /*device*/, 0 /*qfi*/, 0 /*queueIndex*/, 0 /*pQueue*/)
	want := bytes.Join([][]byte{
		le32(17), le32(0), le64(0xD0), le32(0), le32(0), le64(0), // pQueue absent
	}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkGetDeviceQueue\n got % x\nwant % x", e.Bytes(), want)
	}
	// present out-handle arm.
	e2 := vncs.NewEncoder()
	Encode_vkGetDeviceQueue(e2, 0, 0xD0, 1, 2, 0x77)
	want2 := bytes.Join([][]byte{
		le32(17), le32(0), le64(0xD0), le32(1), le32(2), le64(1), le64(0x77),
	}, nil)
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("vkGetDeviceQueue present\n got % x\nwant % x", e2.Bytes(), want2)
	}
}

// TestDecodeVkGetDeviceQueueReply crafts the reply from
// vn_decode_vkGetDeviceQueue_reply: cmd-type echo (17) + simple_pointer(pQueue)
// + VkQueue id. There is NO VkResult (vkGetDeviceQueue returns void).
func TestDecodeVkGetDeviceQueueReply(t *testing.T) {
	var s []byte
	s = append(s, le32(17)...)     // echoed cmd_type
	s = append(s, le64(1)...)      // simple_pointer present
	s = append(s, le64(0xABBA)...) // VkQueue id
	cmdType, queue, ok := Decode_vkGetDeviceQueue_reply(vncs.NewDecoder(s))
	if cmdType != VkGetDeviceQueueReplyCmdType || !ok || queue != 0xABBA {
		t.Fatalf("reply = (%d,%#x,%v)", cmdType, queue, ok)
	}
	// absent arm.
	s2 := bytes.Join([][]byte{le32(17), le64(0)}, nil)
	_, q2, ok2 := Decode_vkGetDeviceQueue_reply(vncs.NewDecoder(s2))
	if ok2 || q2 != 0 {
		t.Fatalf("absent arm = (%#x,%v)", q2, ok2)
	}
}

// ---- vkGetImageMemoryRequirements (partial encode + struct reply) ----

// TestEncodeVkGetImageMemoryRequirements derives the request from
// vn_encode_vkGetImageMemoryRequirements: cmd_type(31) + cmd_flags + VkDevice +
// VkImage + simple_pointer(pMemoryRequirements) + the _partial skeleton. The
// VkMemoryRequirements _partial skips all three scalar members, so it encodes
// NOTHING past the simple_pointer.
func TestEncodeVkGetImageMemoryRequirements(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkGetImageMemoryRequirements(e, 0, 0xD0, 0x20 /*image*/, &VkMemoryRequirements{})
	want := bytes.Join([][]byte{
		le32(31), le32(0), le64(0xD0), le64(0x20),
		le64(1), // simple_pointer(pMemoryRequirements) present; _partial emits nothing
	}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkGetImageMemoryRequirements\n got % x\nwant % x", e.Bytes(), want)
	}
	// NULL out-struct arm.
	e2 := vncs.NewEncoder()
	Encode_vkGetImageMemoryRequirements(e2, 0, 0xD0, 0x20, nil)
	want2 := bytes.Join([][]byte{le32(31), le32(0), le64(0xD0), le64(0x20), le64(0)}, nil)
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("vkGetImageMemoryRequirements nil\n got % x\nwant % x", e2.Bytes(), want2)
	}
}

// TestEncodeVkMemoryRequirementsPartial validates the _partial skeleton emits
// nothing (Mesa vn_encode_VkMemoryRequirements_partial skips every member).
func TestEncodeVkMemoryRequirementsPartial(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkMemoryRequirementsPartial(e, &VkMemoryRequirements{Size: 0x4000, Alignment: 0x100, MemoryTypeBits: 0x7})
	if len(e.Bytes()) != 0 {
		t.Fatalf("partial should emit nothing, got % x", e.Bytes())
	}
}

// TestDecodeVkGetImageMemoryRequirementsReply crafts the reply from
// vn_decode_vkGetImageMemoryRequirements_reply: cmd-type echo (31) +
// simple_pointer + the FULL VkMemoryRequirements decode (no VkResult).
func TestDecodeVkGetImageMemoryRequirementsReply(t *testing.T) {
	var s []byte
	s = append(s, le32(31)...)     // echoed cmd_type
	s = append(s, le64(1)...)      // simple_pointer present
	s = append(s, le64(0x4000)...) // size
	s = append(s, le64(0x100)...)  // alignment
	s = append(s, le32(0x7)...)    // memoryTypeBits
	var mr VkMemoryRequirements
	cmdType, ok := Decode_vkGetImageMemoryRequirements_reply(vncs.NewDecoder(s), &mr)
	if cmdType != VkGetImageMemoryRequirementsReplyCmdType || !ok {
		t.Fatalf("reply = (%d,%v)", cmdType, ok)
	}
	if mr.Size != 0x4000 || mr.Alignment != 0x100 || mr.MemoryTypeBits != 0x7 {
		t.Fatalf("mr = %+v", mr)
	}
	// absent arm.
	_, ok2 := Decode_vkGetImageMemoryRequirements_reply(vncs.NewDecoder(bytes.Join([][]byte{le32(31), le64(0)}, nil)), &mr)
	if ok2 {
		t.Fatal("expected absent struct arm")
	}
}

// ---- vkBindImageMemory (encode + result reply) ----

func TestEncodeVkBindImageMemory(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkBindImageMemory(e, 0, 0xD0, 0x20 /*image*/, 0x30 /*memory*/, 0x1000 /*offset*/)
	want := bytes.Join([][]byte{
		le32(29), le32(0), le64(0xD0), le64(0x20), le64(0x30), le64(0x1000),
	}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkBindImageMemory\n got % x\nwant % x", e.Bytes(), want)
	}
}

func TestDecodeVkBindImageMemoryReply(t *testing.T) {
	s := bytes.Join([][]byte{le32(29), le32(0)}, nil) // cmd echo + VK_SUCCESS
	cmdType, result := Decode_vkBindImageMemory_reply(vncs.NewDecoder(s))
	if cmdType != VkBindImageMemoryReplyCmdType || result != 0 {
		t.Fatalf("reply = (%d,%d)", cmdType, result)
	}
	// error result arm.
	_, r2 := Decode_vkBindImageMemory_reply(vncs.NewDecoder(bytes.Join([][]byte{le32(29), le32(0xFFFFFFFF)}, nil)))
	if r2 != -1 {
		t.Fatalf("error result = %d want -1", r2)
	}
}

// ---- vkAllocateCommandBuffers (struct-field-counted handle array) ----

// TestEncodeVkAllocateCommandBuffers derives the request from
// vn_encode_vkAllocateCommandBuffers: cmd_type(88) + cmd_flags + VkDevice +
// simple_pointer(pAllocateInfo)+VkCommandBufferAllocateInfo + the handle array
// whose count is pAllocateInfo->commandBufferCount (NOT the slice length).
func TestEncodeVkAllocateCommandBuffers(t *testing.T) {
	ai := &VkCommandBufferAllocateInfo{CommandPool: 0xCAFE, Level: 0, CommandBufferCount: 2}
	e := vncs.NewEncoder()
	Encode_vkAllocateCommandBuffers(e, 0, 0xD0, ai, []uint64{0, 0}) // out handles 0 (request side)

	var info []byte
	info = append(info, le32(40)...)     // sType COMMAND_BUFFER_ALLOCATE_INFO
	info = append(info, le64(0)...)      // pNext NULL
	info = append(info, le64(0xCAFE)...) // commandPool handle
	info = append(info, le32(0)...)      // level enum
	info = append(info, le32(2)...)      // commandBufferCount

	var want []byte
	want = append(want, le32(88)...)
	want = append(want, le32(0)...)
	want = append(want, le64(0xD0)...)
	want = append(want, le64(1)...) // simple_pointer(pAllocateInfo)
	want = append(want, info...)
	want = append(want, le64(2)...) // array_size = commandBufferCount (struct field)
	want = append(want, le64(0)...) // pCommandBuffers[0]
	want = append(want, le64(0)...) // pCommandBuffers[1]
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkAllocateCommandBuffers\n got % x\nwant % x", e.Bytes(), want)
	}

	// empty out-array arm (array_size(0)); also nil pAllocateInfo -> count 0
	// path is unreachable here since len(slice)==0 takes the else branch.
	e2 := vncs.NewEncoder()
	Encode_vkAllocateCommandBuffers(e2, 0, 0xD0, ai, nil)
	var want2 []byte
	want2 = append(want2, le32(88)...)
	want2 = append(want2, le32(0)...)
	want2 = append(want2, le64(0xD0)...)
	want2 = append(want2, le64(1)...)
	want2 = append(want2, info...)
	want2 = append(want2, le64(0)...) // empty -> array_size(0)
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("vkAllocateCommandBuffers empty\n got % x\nwant % x", e2.Bytes(), want2)
	}

	// nil pAllocateInfo with a present array exercises the `n = 0` (struct nil)
	// branch: simple_pointer(pAllocateInfo NULL) then array_size(0).
	e3 := vncs.NewEncoder()
	Encode_vkAllocateCommandBuffers(e3, 0, 0xD0, nil, []uint64{0})
	want3 := bytes.Join([][]byte{le32(88), le32(0), le64(0xD0), le64(0), le64(0)}, nil)
	if !bytes.Equal(e3.Bytes(), want3) {
		t.Fatalf("vkAllocateCommandBuffers nil-info\n got % x\nwant % x", e3.Bytes(), want3)
	}
}

// TestDecodeVkAllocateCommandBuffersReply crafts the reply from
// vn_decode_vkAllocateCommandBuffers_reply: cmd-type echo (88) + VkResult +
// peeked counted handle array bounded by maxCount (commandBufferCount).
func TestDecodeVkAllocateCommandBuffersReply(t *testing.T) {
	var s []byte
	s = append(s, le32(88)...)   // cmd echo
	s = append(s, le32(0)...)    // VK_SUCCESS
	s = append(s, le64(2)...)    // array_size(2)
	s = append(s, le64(0xC1)...) // pCommandBuffers[0]
	s = append(s, le64(0xC2)...) // pCommandBuffers[1]
	cmdType, result, bufs := Decode_vkAllocateCommandBuffers_reply(vncs.NewDecoder(s), 2)
	if cmdType != VkAllocateCommandBuffersReplyCmdType || result != 0 {
		t.Fatalf("reply head = (%d,%d)", cmdType, result)
	}
	if len(bufs) != 2 || bufs[0] != 0xC1 || bufs[1] != 0xC2 {
		t.Fatalf("bufs = %#x", bufs)
	}
	// empty-array arm: array_size(0) consumed, nil slice.
	s2 := bytes.Join([][]byte{le32(88), le32(0), le64(0)}, nil)
	_, _, bufs2 := Decode_vkAllocateCommandBuffers_reply(vncs.NewDecoder(s2), 2)
	if bufs2 != nil {
		t.Fatalf("expected nil bufs, got %#x", bufs2)
	}
}

// ---- vkBeginCommandBuffer / vkEndCommandBuffer ----

// TestEncodeVkCommandBufferBeginInfo derives the struct from
// vn_encode_VkCommandBufferBeginInfo / _self: sType(42) + pNext NULL +
// VkFlags flags + simple_pointer(pInheritanceInfo) (NULL for a primary buffer).
func TestEncodeVkCommandBufferBeginInfo(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkCommandBufferBeginInfo(e, &VkCommandBufferBeginInfo{Flags: 0x1 /*ONE_TIME_SUBMIT*/})
	want := bytes.Join([][]byte{le32(42), le64(0), le32(0x1), le64(0)}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkCommandBufferBeginInfo\n got % x\nwant % x", e.Bytes(), want)
	}
	// present pInheritanceInfo arm (secondary command buffer).
	ih := &VkCommandBufferInheritanceInfo{
		RenderPass: 0x11, Subpass: 1, Framebuffer: 0x22,
		OcclusionQueryEnable: true, QueryFlags: 0x4, PipelineStatistics: 0x8,
	}
	e2 := vncs.NewEncoder()
	EncodeVkCommandBufferBeginInfo(e2, &VkCommandBufferBeginInfo{Flags: 0x2, PInheritanceInfo: ih})
	var want2 []byte
	want2 = append(want2, le32(42)...)  // sType
	want2 = append(want2, le64(0)...)   // pNext NULL
	want2 = append(want2, le32(0x2)...) // flags
	want2 = append(want2, le64(1)...)   // simple_pointer(pInheritanceInfo) present
	// nested VkCommandBufferInheritanceInfo:
	want2 = append(want2, le32(41)...)   // sType INHERITANCE_INFO
	want2 = append(want2, le64(0)...)    // pNext NULL
	want2 = append(want2, le64(0x11)...) // renderPass handle
	want2 = append(want2, le32(1)...)    // subpass
	want2 = append(want2, le64(0x22)...) // framebuffer handle
	want2 = append(want2, le32(1)...)    // occlusionQueryEnable (VkBool32)
	want2 = append(want2, le32(0x4)...)  // queryFlags
	want2 = append(want2, le32(0x8)...)  // pipelineStatistics
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("BeginInfo w/ inheritance\n got % x\nwant % x", e2.Bytes(), want2)
	}
}

// TestEncodeVkBeginCommandBuffer derives the command from
// vn_encode_vkBeginCommandBuffer: cmd_type(90) + cmd_flags + VkCommandBuffer +
// simple_pointer(pBeginInfo) + VkCommandBufferBeginInfo.
func TestEncodeVkBeginCommandBuffer(t *testing.T) {
	bi := &VkCommandBufferBeginInfo{Flags: 0x1}
	e := vncs.NewEncoder()
	Encode_vkBeginCommandBuffer(e, 0, 0x10 /*cmdbuf*/, bi)
	var want []byte
	want = append(want, le32(90)...)
	want = append(want, le32(0)...)
	want = append(want, le64(0x10)...)
	want = append(want, le64(1)...) // simple_pointer(pBeginInfo)
	want = append(want, bytes.Join([][]byte{le32(42), le64(0), le32(0x1), le64(0)}, nil)...)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkBeginCommandBuffer\n got % x\nwant % x", e.Bytes(), want)
	}
	// NULL pBeginInfo arm.
	e2 := vncs.NewEncoder()
	Encode_vkBeginCommandBuffer(e2, 0, 0x10, nil)
	want2 := bytes.Join([][]byte{le32(90), le32(0), le64(0x10), le64(0)}, nil)
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("vkBeginCommandBuffer nil\n got % x\nwant % x", e2.Bytes(), want2)
	}
}

func TestDecodeVkBeginEndCommandBufferReply(t *testing.T) {
	cmdType, result := Decode_vkBeginCommandBuffer_reply(vncs.NewDecoder(bytes.Join([][]byte{le32(90), le32(0)}, nil)))
	if cmdType != VkBeginCommandBufferReplyCmdType || result != 0 {
		t.Fatalf("begin reply = (%d,%d)", cmdType, result)
	}
	cmdType2, result2 := Decode_vkEndCommandBuffer_reply(vncs.NewDecoder(bytes.Join([][]byte{le32(91), le32(0)}, nil)))
	if cmdType2 != VkEndCommandBufferReplyCmdType || result2 != 0 {
		t.Fatalf("end reply = (%d,%d)", cmdType2, result2)
	}
}

// TestEncodeVkEndCommandBuffer derives the command from
// vn_encode_vkEndCommandBuffer: cmd_type(91) + cmd_flags + VkCommandBuffer.
func TestEncodeVkEndCommandBuffer(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkEndCommandBuffer(e, 0, 0x10)
	want := bytes.Join([][]byte{le32(91), le32(0), le64(0x10)}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkEndCommandBuffer\n got % x\nwant % x", e.Bytes(), want)
	}
}

// ---- queue / fence completion path ----

// TestEncodeVkQueueWaitIdle derives the command from vn_encode_vkQueueWaitIdle:
// cmd_type(19) + cmd_flags + VkQueue(handle).
func TestEncodeVkQueueWaitIdle(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkQueueWaitIdle(e, 0, 0x10 /*queue*/)
	want := bytes.Join([][]byte{le32(19), le32(0), le64(0x10)}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkQueueWaitIdle\n got % x\nwant % x", e.Bytes(), want)
	}
	cmdType, result := Decode_vkQueueWaitIdle_reply(vncs.NewDecoder(bytes.Join([][]byte{le32(19), le32(0)}, nil)))
	if cmdType != VkQueueWaitIdleReplyCmdType || result != 0 {
		t.Fatalf("waitidle reply = (%d,%d)", cmdType, result)
	}
}

// TestEncodeVkWaitForFences derives the command from vn_encode_vkWaitForFences:
// cmd_type(39) + cmd_flags + VkDevice + uint32 fenceCount + counted VkFence
// array + VkBool32 waitAll + uint64 timeout.
func TestEncodeVkWaitForFences(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkWaitForFences(e, 0, 0xD0, 1, []uint64{0xF1}, true, 0xFFFFFFFFFFFFFFFF)
	var want []byte
	want = append(want, le32(39)...)
	want = append(want, le32(0)...)
	want = append(want, le64(0xD0)...)
	want = append(want, le32(1)...)    // fenceCount
	want = append(want, le64(1)...)    // array_size(1)
	want = append(want, le64(0xF1)...) // pFences[0]
	want = append(want, le32(1)...)    // waitAll = true
	want = append(want, le64(0xFFFFFFFFFFFFFFFF)...)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkWaitForFences\n got % x\nwant % x", e.Bytes(), want)
	}
	cmdType, result := Decode_vkWaitForFences_reply(vncs.NewDecoder(bytes.Join([][]byte{le32(39), le32(0)}, nil)))
	if cmdType != VkWaitForFencesReplyCmdType || result != 0 {
		t.Fatalf("waitfences reply = (%d,%d)", cmdType, result)
	}
	// empty fence array arm (array_size(0)) + waitAll false.
	e2 := vncs.NewEncoder()
	Encode_vkWaitForFences(e2, 0, 0xD0, 0, nil, false, 0)
	want2 := bytes.Join([][]byte{le32(39), le32(0), le64(0xD0), le32(0), le64(0), le32(0), le64(0)}, nil)
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("vkWaitForFences empty\n got % x\nwant % x", e2.Bytes(), want2)
	}
}

// ---- vkGetPhysicalDeviceMemoryProperties (partial encode + struct reply) ----

// memTypePartialEmpty / memHeapPartialEmpty document that each element _partial
// emits nothing; the memory-properties _partial therefore emits only the two
// array_size prefixes (VK_MAX_MEMORY_TYPES=32, VK_MAX_MEMORY_HEAPS=16).
func TestEncodeVkPhysicalDeviceMemoryPropertiesPartial(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkPhysicalDeviceMemoryPropertiesPartial(e, &VkPhysicalDeviceMemoryProperties{})
	want := bytes.Join([][]byte{le64(32), le64(16)}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("memprops partial\n got % x\nwant % x", e.Bytes(), want)
	}
	// element partials emit nothing.
	em := vncs.NewEncoder()
	EncodeVkMemoryTypePartial(em, &VkMemoryType{PropertyFlags: 0xf, HeapIndex: 1})
	eh := vncs.NewEncoder()
	EncodeVkMemoryHeapPartial(eh, &VkMemoryHeap{Size: 0x100, Flags: 0x1})
	if len(em.Bytes()) != 0 || len(eh.Bytes()) != 0 {
		t.Fatalf("element partials should be empty: % x / % x", em.Bytes(), eh.Bytes())
	}
}

// TestEncodeVkGetPhysicalDeviceMemoryProperties derives the request from
// vn_encode_vkGetPhysicalDeviceMemoryProperties: cmd_type(8) + cmd_flags +
// VkPhysicalDevice + simple_pointer(pMemoryProperties) + the _partial skeleton.
func TestEncodeVkGetPhysicalDeviceMemoryProperties(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkGetPhysicalDeviceMemoryProperties(e, 0, 0x01 /*physdev*/, &VkPhysicalDeviceMemoryProperties{})
	want := bytes.Join([][]byte{
		le32(8), le32(0), le64(0x01),
		le64(1),            // simple_pointer present
		le64(32), le64(16), // _partial: the two array_size prefixes
	}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkGetPhysicalDeviceMemoryProperties\n got % x\nwant % x", e.Bytes(), want)
	}
	// NULL out-struct arm.
	e2 := vncs.NewEncoder()
	Encode_vkGetPhysicalDeviceMemoryProperties(e2, 0, 0x01, nil)
	want2 := bytes.Join([][]byte{le32(8), le32(0), le64(0x01), le64(0)}, nil)
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("vkGetPhysicalDeviceMemoryProperties nil\n got % x\nwant % x", e2.Bytes(), want2)
	}
}

// TestDecodeVkPhysicalDeviceMemoryProperties hand-derives the FULL decode from
// vn_decode_VkPhysicalDeviceMemoryProperties: uint32 memoryTypeCount +
// array_size(32)+N*VkMemoryType + uint32 memoryHeapCount + array_size(16)+
// M*VkMemoryHeap. Here the renderer sends 2 types and 1 heap (the wire
// array_size IS the iteration count).
func TestDecodeVkPhysicalDeviceMemoryProperties(t *testing.T) {
	var s []byte
	s = append(s, le32(2)...) // memoryTypeCount
	s = append(s, le64(2)...) // array_size(2) (renderer fills only the live slots)
	// VkMemoryType[0]: propertyFlags, heapIndex
	s = append(s, le32(0x6)...) // HOST_VISIBLE|HOST_COHERENT-ish
	s = append(s, le32(0)...)
	// VkMemoryType[1]
	s = append(s, le32(0x1)...) // DEVICE_LOCAL
	s = append(s, le32(1)...)
	s = append(s, le32(1)...)          // memoryHeapCount
	s = append(s, le64(1)...)          // array_size(1)
	s = append(s, le64(0x80000000)...) // VkMemoryHeap[0].size
	s = append(s, le32(0x1)...)        // VkMemoryHeap[0].flags (DEVICE_LOCAL)

	var p VkPhysicalDeviceMemoryProperties
	dec := vncs.NewDecoder(s)
	DecodeVkPhysicalDeviceMemoryProperties(dec, &p)
	if dec.Remaining() != 0 || dec.Fatal() {
		t.Fatalf("remaining=%d fatal=%v", dec.Remaining(), dec.Fatal())
	}
	if p.MemoryTypeCount != 2 || p.MemoryHeapCount != 1 {
		t.Fatalf("counts = %d/%d", p.MemoryTypeCount, p.MemoryHeapCount)
	}
	if p.MemoryTypes[0].PropertyFlags != 0x6 || p.MemoryTypes[1].HeapIndex != 1 {
		t.Fatalf("memoryTypes = %+v", p.MemoryTypes[:2])
	}
	if p.MemoryHeaps[0].Size != 0x80000000 || p.MemoryHeaps[0].Flags != 0x1 {
		t.Fatalf("memoryHeaps[0] = %+v", p.MemoryHeaps[0])
	}
}

// TestDecodeVkGetPhysicalDeviceMemoryPropertiesReply wraps the struct decode in
// the reply framing (cmd echo 8 + simple_pointer + struct, no VkResult).
func TestDecodeVkGetPhysicalDeviceMemoryPropertiesReply(t *testing.T) {
	var s []byte
	s = append(s, le32(8)...) // cmd echo
	s = append(s, le64(1)...) // simple_pointer present
	s = append(s, le32(1)...) // memoryTypeCount
	s = append(s, le64(1)...) // array_size(1)
	s = append(s, le32(0x1)...)
	s = append(s, le32(0)...)
	s = append(s, le32(1)...) // memoryHeapCount
	s = append(s, le64(1)...) // array_size(1)
	s = append(s, le64(0x100)...)
	s = append(s, le32(0x1)...)
	var p VkPhysicalDeviceMemoryProperties
	cmdType, ok := Decode_vkGetPhysicalDeviceMemoryProperties_reply(vncs.NewDecoder(s), &p)
	if cmdType != VkGetPhysicalDeviceMemoryPropertiesReplyCmdType || !ok {
		t.Fatalf("reply = (%d,%v)", cmdType, ok)
	}
	if p.MemoryTypeCount != 1 || p.MemoryHeaps[0].Size != 0x100 {
		t.Fatalf("decoded = %+v", p)
	}
	// absent arm.
	_, ok2 := Decode_vkGetPhysicalDeviceMemoryProperties_reply(vncs.NewDecoder(bytes.Join([][]byte{le32(8), le64(0)}, nil)), &p)
	if ok2 {
		t.Fatal("expected absent arm")
	}
}

// ---- vkGetPhysicalDeviceQueueFamilyProperties (count + struct array) ----

// TestEncodeVkGetPhysicalDeviceQueueFamilyProperties derives the request from
// vn_encode_vkGetPhysicalDeviceQueueFamilyProperties: cmd_type(7) + cmd_flags +
// VkPhysicalDevice + simple_pointer(pCount)+uint32 + counted struct array
// (each VkQueueFamilyProperties as its _partial, which is the nested VkExtent3D
// _partial = nothing). So with N=1 the array is array_size(1) and no payload.
func TestEncodeVkGetPhysicalDeviceQueueFamilyProperties(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkGetPhysicalDeviceQueueFamilyProperties(e, 0, 0x01, 1, []VkQueueFamilyProperties{{}})
	want := bytes.Join([][]byte{
		le32(7), le32(0), le64(0x01),
		le64(1), le32(1), // simple_pointer(pCount) + count value 1
		le64(1), // array_size(1); each element _partial emits nothing
	}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkGetPhysicalDeviceQueueFamilyProperties\n got % x\nwant % x", e.Bytes(), want)
	}
	// count-query arm (no array).
	e2 := vncs.NewEncoder()
	Encode_vkGetPhysicalDeviceQueueFamilyProperties(e2, 0, 0x01, 0, nil)
	want2 := bytes.Join([][]byte{le32(7), le32(0), le64(0x01), le64(1), le32(0), le64(0)}, nil)
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("queuefamily count-query\n got % x\nwant % x", e2.Bytes(), want2)
	}
}

// TestEncodeVkQueueFamilyPropertiesPartial validates the _partial (only the
// nested VkExtent3D _partial, which emits nothing).
func TestEncodeVkQueueFamilyPropertiesPartial(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkQueueFamilyPropertiesPartial(e, &VkQueueFamilyProperties{
		QueueFlags: 0xf, QueueCount: 4, TimestampValidBits: 64,
		MinImageTransferGranularity: VkExtent3D{Width: 1, Height: 1, Depth: 1},
	})
	if len(e.Bytes()) != 0 {
		t.Fatalf("queuefamily partial should emit nothing, got % x", e.Bytes())
	}
}

// TestDecodeVkQueueFamilyProperties hand-derives the FULL decode from
// vn_decode_VkQueueFamilyProperties: VkFlags queueFlags + uint32 queueCount +
// uint32 timestampValidBits + nested VkExtent3D.
func TestDecodeVkQueueFamilyProperties(t *testing.T) {
	var s []byte
	s = append(s, le32(0xf)...) // queueFlags (GRAPHICS|COMPUTE|TRANSFER|SPARSE)
	s = append(s, le32(16)...)  // queueCount
	s = append(s, le32(64)...)  // timestampValidBits
	s = append(s, le32(1)...)   // minImageTransferGranularity.width
	s = append(s, le32(1)...)   // .height
	s = append(s, le32(1)...)   // .depth
	var q VkQueueFamilyProperties
	dec := vncs.NewDecoder(s)
	DecodeVkQueueFamilyProperties(dec, &q)
	if dec.Remaining() != 0 || dec.Fatal() {
		t.Fatalf("remaining=%d fatal=%v", dec.Remaining(), dec.Fatal())
	}
	if q.QueueFlags != 0xf || q.QueueCount != 16 || q.TimestampValidBits != 64 {
		t.Fatalf("q = %+v", q)
	}
	if q.MinImageTransferGranularity != (VkExtent3D{1, 1, 1}) {
		t.Fatalf("granularity = %+v", q.MinImageTransferGranularity)
	}
}

// TestDecodeVkGetPhysicalDeviceQueueFamilyPropertiesReply crafts the reply from
// vn_decode_vkGetPhysicalDeviceQueueFamilyProperties_reply: cmd echo (7) +
// simple_pointer(pCount)+uint32 + peeked counted struct array (no VkResult).
func TestDecodeVkGetPhysicalDeviceQueueFamilyPropertiesReply(t *testing.T) {
	var s []byte
	s = append(s, le32(7)...) // cmd echo
	s = append(s, le64(1)...) // simple_pointer(pCount) present
	s = append(s, le32(1)...) // *pCount = 1
	s = append(s, le64(1)...) // array_size(1)
	// VkQueueFamilyProperties[0]:
	s = append(s, le32(0x7)...) // queueFlags
	s = append(s, le32(1)...)   // queueCount
	s = append(s, le32(0)...)   // timestampValidBits
	s = append(s, le32(1)...)   // granularity.width
	s = append(s, le32(1)...)   // .height
	s = append(s, le32(1)...)   // .depth
	cmdType, count, countOK, fams := Decode_vkGetPhysicalDeviceQueueFamilyProperties_reply(vncs.NewDecoder(s))
	if cmdType != VkGetPhysicalDeviceQueueFamilyPropertiesReplyCmdType || !countOK || count != 1 {
		t.Fatalf("reply head = (%d,%v,%d)", cmdType, countOK, count)
	}
	if len(fams) != 1 || fams[0].QueueFlags != 0x7 || fams[0].QueueCount != 1 {
		t.Fatalf("fams = %+v", fams)
	}
	// count-query arm (array_size(0) consumed, nil slice).
	s2 := bytes.Join([][]byte{le32(7), le64(1), le32(8), le64(0)}, nil)
	_, c2, _, fams2 := Decode_vkGetPhysicalDeviceQueueFamilyProperties_reply(vncs.NewDecoder(s2))
	if c2 != 8 || fams2 != nil {
		t.Fatalf("count-query arm = (%d, %+v)", c2, fams2)
	}
	// absent-count arm (simple_pointer(0)).
	s3 := bytes.Join([][]byte{le32(7), le64(0), le64(0)}, nil)
	_, _, ok3, _ := Decode_vkGetPhysicalDeviceQueueFamilyProperties_reply(vncs.NewDecoder(s3))
	if ok3 {
		t.Fatal("expected absent-count arm")
	}
}
