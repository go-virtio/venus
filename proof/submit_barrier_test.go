package proof

import (
	"bytes"
	"testing"

	"github.com/go-virtio/venus/internal/vncs"
)

// These tests cover the pNext-chain encoders (Gap 2) and the
// vkQueueSubmit / vkCmdPipelineBarrier framing (Gap 3), with every expected
// byte stream hand-derived from Mesa's venus-protocol encoders
// (vn_encode_VkSubmitInfo[/_pnext/_self], vn_encode_VkImageMemoryBarrier[...],
// vn_encode_vkQueueSubmit, vn_encode_vkCmdPipelineBarrier in
// vn_protocol_driver_{queue,command_buffer}.h). None reference the generated
// code; they are built field-by-field with le32/le64 in Mesa field order.

// ---- pNext chain: VkImageMemoryBarrier (empty chain) ----

// imageMemoryBarrierExpected hand-derives the bytes for an imb whose pNext is
// empty, per vn_encode_VkImageMemoryBarrier / _self:
//
//	sType(45) + pNext-chain(sp(0)) + VkFlags srcAccessMask + VkFlags dstAccessMask
//	+ VkImageLayout oldLayout(int32) + newLayout(int32) + uint32 srcQFI
//	+ uint32 dstQFI + VkImage(handle) + nested VkImageSubresourceRange.
func imageMemoryBarrierExpected(imb *VkImageMemoryBarrier, pnext []byte) []byte {
	var w []byte
	w = append(w, le32(45)...) // sType IMAGE_MEMORY_BARRIER
	w = append(w, pnext...)    // pNext chain
	w = append(w, le32(imb.SrcAccessMask)...)
	w = append(w, le32(imb.DstAccessMask)...)
	w = append(w, le32(uint32(imb.OldLayout))...)
	w = append(w, le32(uint32(imb.NewLayout))...)
	w = append(w, le32(imb.SrcQueueFamilyIndex)...)
	w = append(w, le32(imb.DstQueueFamilyIndex)...)
	w = append(w, le64(imb.Image)...)
	r := imb.SubresourceRange
	w = append(w, le32(r.AspectMask)...)
	w = append(w, le32(r.BaseMipLevel)...)
	w = append(w, le32(r.LevelCount)...)
	w = append(w, le32(r.BaseArrayLayer)...)
	w = append(w, le32(r.LayerCount)...)
	return w
}

func TestEncodeVkImageMemoryBarrierEmptyChain(t *testing.T) {
	imb := &VkImageMemoryBarrier{
		SrcAccessMask:       0x40, // VK_ACCESS_TRANSFER_WRITE_BIT-ish
		DstAccessMask:       0x80,
		OldLayout:           1, // VK_IMAGE_LAYOUT_GENERAL
		NewLayout:           7, // VK_IMAGE_LAYOUT_TRANSFER_DST_OPTIMAL
		SrcQueueFamilyIndex: 0,
		DstQueueFamilyIndex: 0,
		Image:               0x55,
		SubresourceRange:    VkImageSubresourceRange{AspectMask: 0x1, LevelCount: 1, LayerCount: 1},
	}
	e := vncs.NewEncoder()
	EncodeVkImageMemoryBarrier(e, imb)
	// empty pNext chain = simple_pointer(NULL) = 8-byte LE 0.
	want := imageMemoryBarrierExpected(imb, le64(0))
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkImageMemoryBarrier (empty chain)\n got % x\nwant % x", e.Bytes(), want)
	}
}

// ---- pNext chain: VkSubmitInfo with a 1-node VkProtectedSubmitInfo chain ----

// submitInfoSelf hand-derives the _self bytes (everything past the pNext chain)
// of a VkSubmitInfo per vn_encode_VkSubmitInfo_self: uint32 waitSemaphoreCount,
// then for each of pWaitSemaphores / pWaitDstStageMask / pCommandBuffers /
// pSignalSemaphores an `if(p){array_size(count); elems} else array_size(0)`.
func submitInfoSelf(si *VkSubmitInfo) []byte {
	var w []byte
	w = append(w, le32(si.WaitSemaphoreCount)...)
	w = append(w, countedHandles(si.PWaitSemaphores)...)
	w = append(w, countedFlags(si.PWaitDstStageMask)...)
	w = append(w, le32(si.CommandBufferCount)...)
	w = append(w, countedHandles(si.PCommandBuffers)...)
	w = append(w, le32(si.SignalSemaphoreCount)...)
	w = append(w, countedHandles(si.PSignalSemaphores)...)
	return w
}

func countedHandles(h []uint64) []byte {
	if len(h) == 0 {
		return le64(0)
	}
	w := le64(uint64(len(h)))
	for _, id := range h {
		w = append(w, le64(id)...)
	}
	return w
}

func countedFlags(f []uint32) []byte {
	if len(f) == 0 {
		return le64(0)
	}
	w := le64(uint64(len(f)))
	for _, v := range f {
		w = append(w, le32(v)...)
	}
	return w
}

func TestEncodeVkSubmitInfoWithProtectedNode(t *testing.T) {
	// A 1-node pNext chain: VkProtectedSubmitInfo{protectedSubmit=true}.
	// Per Mesa vn_encode_VkSubmitInfo_pnext for that node:
	//   sp(1) sType(PROTECTED_SUBMIT_INFO=1000145000) [recurse(nil): sp(0)] selfNode
	// where selfNode = vn_encode_VkProtectedSubmitInfo_self = VkBool32(protectedSubmit).
	psi := &VkProtectedSubmitInfo{ProtectedSubmit: true}
	si := &VkSubmitInfo{
		PNext:                []vncs.PNextNode{VkProtectedSubmitInfoNode(psi)},
		WaitSemaphoreCount:   1,
		PWaitSemaphores:      []uint64{0xA1},
		PWaitDstStageMask:    []uint32{0x2000}, // VK_PIPELINE_STAGE_TRANSFER_BIT-ish
		CommandBufferCount:   1,
		PCommandBuffers:      []uint64{0xC1},
		SignalSemaphoreCount: 1,
		PSignalSemaphores:    []uint64{0xB1},
	}

	e := vncs.NewEncoder()
	EncodeVkSubmitInfo(e, si)

	var chain []byte
	chain = append(chain, le64(1)...)          // sp(1): node present
	chain = append(chain, le32(1000145000)...) // sType PROTECTED_SUBMIT_INFO
	chain = append(chain, le64(0)...)          // recurse(nil): sp(0) end of chain
	chain = append(chain, le32(1)...)          // selfNode: VkBool32 protectedSubmit=true

	var want []byte
	want = append(want, le32(4)...) // sType SUBMIT_INFO
	want = append(want, chain...)
	want = append(want, submitInfoSelf(si)...)

	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkSubmitInfo (1-node chain)\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkSubmitInfoEmptyArrays exercises the empty handle/flags array arms
// and an empty pNext chain.
func TestEncodeVkSubmitInfoEmptyArrays(t *testing.T) {
	si := &VkSubmitInfo{}
	e := vncs.NewEncoder()
	EncodeVkSubmitInfo(e, si)
	var want []byte
	want = append(want, le32(4)...) // sType
	want = append(want, le64(0)...) // empty pNext chain
	want = append(want, submitInfoSelf(si)...)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkSubmitInfo (empty)\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkProtectedSubmitInfoSelf validates the node self-encoder directly.
func TestEncodeVkProtectedSubmitInfoSelf(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkProtectedSubmitInfoSelf(e, &VkProtectedSubmitInfo{ProtectedSubmit: true})
	if !bytes.Equal(e.Bytes(), le32(1)) {
		t.Fatalf("ProtectedSubmitInfo self\n got % x\nwant % x", e.Bytes(), le32(1))
	}
}

// ---- vkQueueSubmit framing ----

// TestEncodeVkQueueSubmit hand-derives the command from
// vn_encode_vkQueueSubmit: cmd_type(18) + cmd_flags + VkQueue(handle) +
// uint32 submitCount + (pSubmits: array_size(N) + each VkSubmitInfo) +
// VkFence(handle).
func TestEncodeVkQueueSubmit(t *testing.T) {
	si := &VkSubmitInfo{
		CommandBufferCount: 1,
		PCommandBuffers:    []uint64{0xC1},
	}
	e := vncs.NewEncoder()
	Encode_vkQueueSubmit(e, 0, 0x10 /*queue*/, 1, []VkSubmitInfo{*si}, 0x20 /*fence*/)

	var siBytes []byte
	siBytes = append(siBytes, le32(4)...) // sType
	siBytes = append(siBytes, le64(0)...) // empty pNext chain
	siBytes = append(siBytes, submitInfoSelf(si)...)

	var want []byte
	want = append(want, le32(18)...)   // cmd_type vkQueueSubmit = 18
	want = append(want, le32(0)...)    // cmd_flags
	want = append(want, le64(0x10)...) // queue (by-value handle)
	want = append(want, le32(1)...)    // submitCount
	want = append(want, le64(1)...)    // array_size(1)
	want = append(want, siBytes...)
	want = append(want, le64(0x20)...) // fence (by-value handle)

	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkQueueSubmit\n got % x\nwant % x", e.Bytes(), want)
	}

	// Empty-submits arm (array_size(0)).
	e2 := vncs.NewEncoder()
	Encode_vkQueueSubmit(e2, 0, 0x10, 0, nil, 0)
	want2 := bytes.Join([][]byte{le32(18), le32(0), le64(0x10), le32(0), le64(0), le64(0)}, nil)
	if !bytes.Equal(e2.Bytes(), want2) {
		t.Fatalf("vkQueueSubmit empty\n got % x\nwant % x", e2.Bytes(), want2)
	}
}

// ---- vkCmdPipelineBarrier framing ----

// TestEncodeVkCmdPipelineBarrier hand-derives the command from
// vn_encode_vkCmdPipelineBarrier: cmd_type(126) + cmd_flags +
// VkCommandBuffer(handle) + 3 VkFlags (src/dst stage, dependency) + three
// (uint32 count + counted struct array) groups for memory/buffer/image
// barriers. Only the image-barrier group is populated here.
func TestEncodeVkCmdPipelineBarrier(t *testing.T) {
	imb := &VkImageMemoryBarrier{
		SrcAccessMask:    0x40,
		DstAccessMask:    0x80,
		OldLayout:        1,
		NewLayout:        7,
		Image:            0x55,
		SubresourceRange: VkImageSubresourceRange{AspectMask: 0x1, LevelCount: 1, LayerCount: 1},
	}
	e := vncs.NewEncoder()
	Encode_vkCmdPipelineBarrier(e, 0,
		0x10,   // commandBuffer
		0x1000, // srcStageMask
		0x2000, // dstStageMask
		0,      // dependencyFlags
		0, nil, // memory barriers
		0, nil, // buffer barriers
		1, []VkImageMemoryBarrier{*imb}, // image barriers
	)

	var want []byte
	want = append(want, le32(126)...)    // cmd_type vkCmdPipelineBarrier = 126
	want = append(want, le32(0)...)      // cmd_flags
	want = append(want, le64(0x10)...)   // commandBuffer (handle)
	want = append(want, le32(0x1000)...) // srcStageMask
	want = append(want, le32(0x2000)...) // dstStageMask
	want = append(want, le32(0)...)      // dependencyFlags
	want = append(want, le32(0)...)      // memoryBarrierCount
	want = append(want, le64(0)...)      // pMemoryBarriers array_size(0)
	want = append(want, le32(0)...)      // bufferMemoryBarrierCount
	want = append(want, le64(0)...)      // pBufferMemoryBarriers array_size(0)
	want = append(want, le32(1)...)      // imageMemoryBarrierCount
	want = append(want, le64(1)...)      // pImageMemoryBarriers array_size(1)
	want = append(want, imageMemoryBarrierExpected(imb, le64(0))...)

	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkCmdPipelineBarrier\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkCmdPipelineBarrierAllEmpty exercises all three empty-array arms.
func TestEncodeVkCmdPipelineBarrierAllEmpty(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkCmdPipelineBarrier(e, 0, 0x10, 0x1, 0x2, 0, 0, nil, 0, nil, 0, nil)
	want := bytes.Join([][]byte{
		le32(126), le32(0), le64(0x10),
		le32(0x1), le32(0x2), le32(0),
		le32(0), le64(0), // memory
		le32(0), le64(0), // buffer
		le32(0), le64(0), // image
	}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkCmdPipelineBarrier empty\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkCmdPipelineBarrierAllPopulated exercises the present-array arm of
// all three barrier groups (memory, buffer, image) in one command, against
// hand-derived bytes.
func TestEncodeVkCmdPipelineBarrierAllPopulated(t *testing.T) {
	mb := VkMemoryBarrier{SrcAccessMask: 0x40, DstAccessMask: 0x80}
	bb := VkBufferMemoryBarrier{
		SrcAccessMask: 0x40, DstAccessMask: 0x80,
		Buffer: 0x33, Offset: 0x100, Size: 0x2000,
	}
	imb := VkImageMemoryBarrier{
		SrcAccessMask: 0x40, DstAccessMask: 0x80, OldLayout: 1, NewLayout: 7,
		Image: 0x55, SubresourceRange: VkImageSubresourceRange{AspectMask: 0x1, LevelCount: 1, LayerCount: 1},
	}
	e := vncs.NewEncoder()
	Encode_vkCmdPipelineBarrier(e, 0, 0x10, 0x1000, 0x2000, 0,
		1, []VkMemoryBarrier{mb},
		1, []VkBufferMemoryBarrier{bb},
		1, []VkImageMemoryBarrier{imb})

	var want []byte
	want = append(want, le32(126)...)
	want = append(want, le32(0)...)
	want = append(want, le64(0x10)...)
	want = append(want, le32(0x1000)...)
	want = append(want, le32(0x2000)...)
	want = append(want, le32(0)...)
	// memory barriers
	want = append(want, le32(1)...)
	want = append(want, le64(1)...)
	want = append(want, bytes.Join([][]byte{le32(46), le64(0), le32(0x40), le32(0x80)}, nil)...)
	// buffer barriers
	want = append(want, le32(1)...)
	want = append(want, le64(1)...)
	want = append(want, bytes.Join([][]byte{
		le32(44), le64(0), le32(0x40), le32(0x80), le32(0), le32(0),
		le64(0x33), le64(0x100), le64(0x2000),
	}, nil)...)
	// image barriers
	want = append(want, le32(1)...)
	want = append(want, le64(1)...)
	want = append(want, imageMemoryBarrierExpected(&imb, le64(0))...)

	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkCmdPipelineBarrier (all populated)\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkMemoryBarrierAndBuffer exercises the two other barrier-node
// encoders (used by the buffer/memory arms of the barrier command), each with
// an empty pNext chain, against hand-derived bytes.
func TestEncodeVkMemoryBarrierAndBuffer(t *testing.T) {
	em := vncs.NewEncoder()
	EncodeVkMemoryBarrier(em, &VkMemoryBarrier{SrcAccessMask: 0x40, DstAccessMask: 0x80})
	wantM := bytes.Join([][]byte{le32(46), le64(0), le32(0x40), le32(0x80)}, nil)
	if !bytes.Equal(em.Bytes(), wantM) {
		t.Fatalf("VkMemoryBarrier\n got % x\nwant % x", em.Bytes(), wantM)
	}

	eb := vncs.NewEncoder()
	EncodeVkBufferMemoryBarrier(eb, &VkBufferMemoryBarrier{
		SrcAccessMask: 0x40, DstAccessMask: 0x80,
		SrcQueueFamilyIndex: 0, DstQueueFamilyIndex: 0,
		Buffer: 0x33, Offset: 0x100, Size: 0x2000,
	})
	wantB := bytes.Join([][]byte{
		le32(44), le64(0), // sType BUFFER_MEMORY_BARRIER + empty chain
		le32(0x40), le32(0x80), le32(0), le32(0),
		le64(0x33),   // buffer handle
		le64(0x100),  // offset (VkDeviceSize)
		le64(0x2000), // size (VkDeviceSize)
	}, nil)
	if !bytes.Equal(eb.Bytes(), wantB) {
		t.Fatalf("VkBufferMemoryBarrier\n got % x\nwant % x", eb.Bytes(), wantB)
	}
}
