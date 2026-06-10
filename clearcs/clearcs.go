// Package clearcs is the public, importable façade over the generated Venus
// clear-image command closure in package proof. proof's exported encoders and
// reply decoders take *vncs.Encoder / *vncs.Decoder, and vncs lives under
// internal/, so proof cannot be driven from another module (e.g. the
// go-virtio/validate/vtest live driver). clearcs re-exports exactly the
// clear-image command set with []byte-in / []byte-out signatures that leak no
// internal types, so an external transport can encode a command stream and
// decode a reply stream without importing internal/vncs.
//
// Every function here is a thin wrapper that constructs a vncs.Encoder /
// vncs.Decoder and calls the SAME proof function the offline byte-derived tests
// (proof/*_test.go) validate; clearcs adds no new wire logic. The struct input
// types are re-exported aliases of the proof types, so the wire bytes are
// produced by the audited generated encoders unchanged.
package clearcs

import (
	"github.com/go-virtio/venus/internal/vncs"
	"github.com/go-virtio/venus/proof"
)

// Re-exported pure-Go input struct types (aliases of the proof types). Using
// aliases (not new types) means callers build the very structs the generated
// encoders consume.
type (
	VkApplicationInfo                = proof.VkApplicationInfo
	VkInstanceCreateInfo             = proof.VkInstanceCreateInfo
	VkDeviceQueueCreateInfo          = proof.VkDeviceQueueCreateInfo
	VkDeviceCreateInfo               = proof.VkDeviceCreateInfo
	VkExtent3D                       = proof.VkExtent3D
	VkImageCreateInfo                = proof.VkImageCreateInfo
	VkMemoryAllocateInfo             = proof.VkMemoryAllocateInfo
	VkCommandPoolCreateInfo          = proof.VkCommandPoolCreateInfo
	VkCommandBufferAllocateInfo      = proof.VkCommandBufferAllocateInfo
	VkCommandBufferBeginInfo         = proof.VkCommandBufferBeginInfo
	VkImageSubresourceRange          = proof.VkImageSubresourceRange
	VkClearColorValue                = proof.VkClearColorValue
	VkSubmitInfo                     = proof.VkSubmitInfo
	VkImageMemoryBarrier             = proof.VkImageMemoryBarrier
	VkMemoryBarrier                  = proof.VkMemoryBarrier
	VkBufferMemoryBarrier            = proof.VkBufferMemoryBarrier
	VkMemoryRequirements             = proof.VkMemoryRequirements
	VkMemoryType                     = proof.VkMemoryType
	VkMemoryHeap                     = proof.VkMemoryHeap
	VkPhysicalDeviceMemoryProperties = proof.VkPhysicalDeviceMemoryProperties
	VkQueueFamilyProperties          = proof.VkQueueFamilyProperties
)

// enc runs a proof encoder closure into a fresh encoder and returns its bytes.
func enc(f func(*vncs.Encoder)) []byte {
	e := vncs.NewEncoder()
	f(e)
	return e.Bytes()
}

// ---- command encoders ([]byte out) ---------------------------------------

func EncodeCreateInstance(cmdFlags uint32, ci *VkInstanceCreateInfo, pInstance uint64) []byte {
	return enc(func(e *vncs.Encoder) { proof.Encode_vkCreateInstance(e, cmdFlags, ci, pInstance) })
}

func EncodeEnumeratePhysicalDevices(cmdFlags uint32, instance uint64, count uint32, devs []uint64) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkEnumeratePhysicalDevices(e, cmdFlags, instance, count, devs)
	})
}

func EncodeGetPhysicalDeviceMemoryProperties(cmdFlags uint32, physDev uint64) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkGetPhysicalDeviceMemoryProperties(e, cmdFlags, physDev, &proof.VkPhysicalDeviceMemoryProperties{})
	})
}

func EncodeGetPhysicalDeviceQueueFamilyProperties(cmdFlags uint32, physDev uint64, count uint32, fams []VkQueueFamilyProperties) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkGetPhysicalDeviceQueueFamilyProperties(e, cmdFlags, physDev, count, fams)
	})
}

// EncodeCreateDevice wraps the generated proof.Encode_vkCreateDevice. The
// generated EncodeVkDeviceCreateInfo now emits the struct's trailing optional
// member
//
//	if (vn_encode_simple_pointer(enc, val->pEnabledFeatures))
//	    vn_encode_VkPhysicalDeviceFeatures(enc, val->pEnabledFeatures);
//
// (Mesa vn_encode_VkDeviceCreateInfo_self, vn_protocol_driver_device.h), so the
// former clearcs-side workaround (encodeCreateDeviceFixed /
// encodeVkDeviceCreateInfoFixed, which re-encoded the command to append the
// missing simple_pointer) is gone: this is now a bare call into the audited
// generated encoder, byte-identical to the old workaround output. Omitting that
// trailing simple_pointer left the host decoder peeking 8 bytes not on the wire
// ("vkr: failed to peek 8 bytes" -> "vkCreateDevice resulted in CS error",
// proven live against a real virgl_test_server --venus host).
func EncodeCreateDevice(cmdFlags uint32, physDev uint64, ci *VkDeviceCreateInfo, pDevice uint64) []byte {
	return enc(func(e *vncs.Encoder) { proof.Encode_vkCreateDevice(e, cmdFlags, physDev, ci, pDevice) })
}

func EncodeGetDeviceQueue(cmdFlags uint32, device uint64, qfi, queueIndex uint32, pQueue uint64) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkGetDeviceQueue(e, cmdFlags, device, qfi, queueIndex, pQueue)
	})
}

func EncodeCreateImage(cmdFlags uint32, device uint64, ci *VkImageCreateInfo, pImage uint64) []byte {
	return enc(func(e *vncs.Encoder) { proof.Encode_vkCreateImage(e, cmdFlags, device, ci, pImage) })
}

func EncodeGetImageMemoryRequirements(cmdFlags uint32, device, image uint64) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkGetImageMemoryRequirements(e, cmdFlags, device, image, &proof.VkMemoryRequirements{})
	})
}

func EncodeAllocateMemory(cmdFlags uint32, device uint64, ai *VkMemoryAllocateInfo, pMemory uint64) []byte {
	return enc(func(e *vncs.Encoder) { proof.Encode_vkAllocateMemory(e, cmdFlags, device, ai, pMemory) })
}

func EncodeBindImageMemory(cmdFlags uint32, device, image, memory, offset uint64) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkBindImageMemory(e, cmdFlags, device, image, memory, offset)
	})
}

func EncodeCreateCommandPool(cmdFlags uint32, device uint64, ci *VkCommandPoolCreateInfo, pPool uint64) []byte {
	return enc(func(e *vncs.Encoder) { proof.Encode_vkCreateCommandPool(e, cmdFlags, device, ci, pPool) })
}

func EncodeAllocateCommandBuffers(cmdFlags uint32, device uint64, ai *VkCommandBufferAllocateInfo, pBufs []uint64) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkAllocateCommandBuffers(e, cmdFlags, device, ai, pBufs)
	})
}

func EncodeBeginCommandBuffer(cmdFlags uint32, cmdBuf uint64, bi *VkCommandBufferBeginInfo) []byte {
	return enc(func(e *vncs.Encoder) { proof.Encode_vkBeginCommandBuffer(e, cmdFlags, cmdBuf, bi) })
}

func EncodeEndCommandBuffer(cmdFlags uint32, cmdBuf uint64) []byte {
	return enc(func(e *vncs.Encoder) { proof.Encode_vkEndCommandBuffer(e, cmdFlags, cmdBuf) })
}

func EncodeCmdPipelineBarrier(cmdFlags uint32, cmdBuf uint64, srcStage, dstStage, depFlags uint32, imgBarriers []VkImageMemoryBarrier) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkCmdPipelineBarrier(e, cmdFlags, cmdBuf, srcStage, dstStage, depFlags,
			0, nil, 0, nil, uint32(len(imgBarriers)), imgBarriers)
	})
}

func EncodeCmdClearColorImage(cmdFlags uint32, cmdBuf, image uint64, layout int32, color *VkClearColorValue, ranges []VkImageSubresourceRange) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkCmdClearColorImage(e, cmdFlags, cmdBuf, image, layout, color, uint32(len(ranges)), ranges)
	})
}

func EncodeQueueSubmit(cmdFlags uint32, queue uint64, submits []VkSubmitInfo, fence uint64) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkQueueSubmit(e, cmdFlags, queue, uint32(len(submits)), submits, fence)
	})
}

func EncodeQueueWaitIdle(cmdFlags uint32, queue uint64) []byte {
	return enc(func(e *vncs.Encoder) { proof.Encode_vkQueueWaitIdle(e, cmdFlags, queue) })
}

// EncodeWaitForFences wraps the generated vkWaitForFences encoder. This is the
// completion barrier Venus actually uses: vn_QueueWaitIdle (vn_queue.c) does NOT
// send a raw vkQueueWaitIdle — it submits an empty batch signalling a fence and
// then waits the fence. We follow that idiom.
func EncodeWaitForFences(cmdFlags uint32, device uint64, fences []uint64, waitAll bool, timeout uint64) []byte {
	return enc(func(e *vncs.Encoder) {
		proof.Encode_vkWaitForFences(e, cmdFlags, device, uint32(len(fences)), fences, waitAll, timeout)
	})
}

// VkFenceCreateInfo is the pure-Go input for EncodeCreateFence. sType and pNext
// are fixed (FENCE_CREATE_INFO / NULL); only flags is a field.
type VkFenceCreateInfo struct {
	Flags uint32
}

// EncodeCreateFence hand-encodes vkCreateFence, which the generated proof set
// does not (yet) cover. The wire form is transcribed from Mesa
// vn_encode_vkCreateFence + vn_encode_VkFenceCreateInfo(_self)
// (src/virtio/venus-protocol/vn_protocol_driver_fence.h):
//
//	i32 cmd_type=35                       (VK_COMMAND_TYPE_vkCreateFence_EXT)
//	u32 cmd_flags
//	u64 device (handle)
//	u64 simple_pointer(pCreateInfo)=1
//	  i32 sType=8                         (VK_STRUCTURE_TYPE_FENCE_CREATE_INFO)
//	  u64 simple_pointer(pNext)=0
//	  u32 flags
//	u64 simple_pointer(pAllocator)=0
//	u64 simple_pointer(pFence)=1
//	  u64 fence id
func EncodeCreateFence(cmdFlags uint32, device uint64, ci *VkFenceCreateInfo, pFence uint64) []byte {
	return enc(func(e *vncs.Encoder) {
		e.EncodeInt32(35) // VK_COMMAND_TYPE_vkCreateFence_EXT
		e.EncodeFlags(cmdFlags)
		e.EncodeHandle(device)
		if e.EncodeSimplePointer(ci != nil) {
			e.EncodeInt32(8)             // sType = FENCE_CREATE_INFO
			e.EncodeSimplePointer(false) // pNext = NULL
			e.EncodeFlags(ci.Flags)
		}
		e.EncodeSimplePointer(false) // pAllocator = NULL
		if e.EncodeSimplePointer(pFence != 0) {
			e.EncodeHandle(pFence)
		}
	})
}

// ---- reply decoders ([]byte in) ------------------------------------------

func DecodeCreateInstanceReply(reply []byte) (result int32, instance uint64, ok bool) {
	_, result, instance, ok = proof.Decode_vkCreateInstance_reply(vncs.NewDecoder(reply))
	return
}

func DecodeEnumeratePhysicalDevicesReply(reply []byte) (result int32, count uint32, countOK bool, devs []uint64) {
	_, result, count, countOK, devs = proof.Decode_vkEnumeratePhysicalDevices_reply(vncs.NewDecoder(reply))
	return
}

func DecodeGetPhysicalDeviceMemoryPropertiesReply(reply []byte) (props VkPhysicalDeviceMemoryProperties, ok bool) {
	_, ok = proof.Decode_vkGetPhysicalDeviceMemoryProperties_reply(vncs.NewDecoder(reply), &props)
	return
}

func DecodeGetPhysicalDeviceQueueFamilyPropertiesReply(reply []byte) (count uint32, countOK bool, fams []VkQueueFamilyProperties) {
	_, count, countOK, fams = proof.Decode_vkGetPhysicalDeviceQueueFamilyProperties_reply(vncs.NewDecoder(reply))
	return
}

func DecodeCreateDeviceReply(reply []byte) (result int32, device uint64, ok bool) {
	_, result, device, ok = proof.Decode_vkCreateDevice_reply(vncs.NewDecoder(reply))
	return
}

func DecodeGetDeviceQueueReply(reply []byte) (queue uint64, ok bool) {
	_, queue, ok = proof.Decode_vkGetDeviceQueue_reply(vncs.NewDecoder(reply))
	return
}

func DecodeCreateImageReply(reply []byte) (result int32, image uint64, ok bool) {
	_, result, image, ok = proof.Decode_vkCreateImage_reply(vncs.NewDecoder(reply))
	return
}

func DecodeGetImageMemoryRequirementsReply(reply []byte) (req VkMemoryRequirements, ok bool) {
	_, ok = proof.Decode_vkGetImageMemoryRequirements_reply(vncs.NewDecoder(reply), &req)
	return
}

func DecodeAllocateMemoryReply(reply []byte) (result int32, memory uint64, ok bool) {
	_, result, memory, ok = proof.Decode_vkAllocateMemory_reply(vncs.NewDecoder(reply))
	return
}

func DecodeBindImageMemoryReply(reply []byte) (result int32) {
	_, result = proof.Decode_vkBindImageMemory_reply(vncs.NewDecoder(reply))
	return
}

func DecodeCreateCommandPoolReply(reply []byte) (result int32, pool uint64, ok bool) {
	_, result, pool, ok = proof.Decode_vkCreateCommandPool_reply(vncs.NewDecoder(reply))
	return
}

func DecodeAllocateCommandBuffersReply(reply []byte, maxCount uint32) (result int32, bufs []uint64) {
	_, result, bufs = proof.Decode_vkAllocateCommandBuffers_reply(vncs.NewDecoder(reply), maxCount)
	return
}

func DecodeBeginCommandBufferReply(reply []byte) (result int32) {
	_, result = proof.Decode_vkBeginCommandBuffer_reply(vncs.NewDecoder(reply))
	return
}

func DecodeEndCommandBufferReply(reply []byte) (result int32) {
	_, result = proof.Decode_vkEndCommandBuffer_reply(vncs.NewDecoder(reply))
	return
}

func DecodeQueueWaitIdleReply(reply []byte) (result int32) {
	_, result = proof.Decode_vkQueueWaitIdle_reply(vncs.NewDecoder(reply))
	return
}

func DecodeWaitForFencesReply(reply []byte) (result int32) {
	_, result = proof.Decode_vkWaitForFences_reply(vncs.NewDecoder(reply))
	return
}

// VkCreateFenceReplyCmdType is VK_COMMAND_TYPE_vkCreateFence_EXT (= 35). The
// reply is cmd_type echo + VkResult + simple_pointer(pFence) + VkFence id,
// matching every other create-style reply (vn_decode_vkCreateFence_reply,
// vn_protocol_driver_fence.h). proof emits no decoder for it, so clearcs
// decodes the create-style reply directly through the public primitives.
const VkCreateFenceReplyCmdType int32 = 35

// DecodeCreateFenceReply decodes the vkCreateFence reply: cmd_type(35) +
// VkResult + simple_pointer(pFence) + VkFence id.
func DecodeCreateFenceReply(reply []byte) (result int32, fence uint64, ok bool) {
	d := vncs.NewDecoder(reply)
	cmdType := d.DecodeInt32()
	result = d.DecodeResult()
	if cmdType != VkCreateFenceReplyCmdType {
		return result, 0, false
	}
	if d.DecodeSimplePointer() {
		fence = d.DecodeHandle()
		ok = true
	}
	return result, fence, ok
}

// VkQueueSubmitReplyCmdType is VK_COMMAND_TYPE_vkQueueSubmit_EXT
// (vn_protocol_driver_defines.h: = 18). The vkQueueSubmit reply is a
// result-only reply (cmd_type echo + VkResult), identical in shape to
// vkBindImageMemory_reply (vn_decode_vkQueueSubmit_reply,
// vn_protocol_driver_queue.h). proof emits no dedicated decoder for it, so
// clearcs decodes the two-field reply directly through the public primitive
// decoders.
const VkQueueSubmitReplyCmdType int32 = 18

// DecodeQueueSubmitReply decodes the result-only vkQueueSubmit reply
// (cmd_type(18) + VkResult). It returns the VkResult and whether the echoed
// cmd_type matched (a wrong cmd_type means the reply slot held something else).
func DecodeQueueSubmitReply(reply []byte) (result int32, cmdTypeOK bool) {
	d := vncs.NewDecoder(reply)
	cmdType := d.DecodeInt32() // echoed VkCommandTypeEXT
	result = d.DecodeInt32()   // VkResult
	return result, cmdType == VkQueueSubmitReplyCmdType
}
