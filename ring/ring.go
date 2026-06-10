// Package ring models the Venus (Vulkan-over-virtio) shared-memory command
// ring — the producer/consumer SPSC ring that carries serialised Vulkan
// command streams from the guest (producer) to the host renderer
// (consumer).
//
// SCOPE AND HONESTY. This package implements the two pieces that are
// VERIFIABLE OFFLINE against Mesa source:
//
//  1. The shared-memory LAYOUT — the byte offsets of head/tail/status and
//     the buffer/extra regions, transcribed from Mesa
//     src/virtio/vulkan/vn_ring.c (vn_ring_get_layout).
//  2. The PRODUCER-SIDE ring algorithm — write command bytes at
//     (cur & buffer_mask), then release-store the monotonic cur into the
//     tail word; busy-poll the head word (acquire-load) for free space.
//     This mirrors vn_ring_submit_command_internal / vn_ring_get_seqno /
//     vn_ring_wait_seqno in vn_ring.c.
//  3. The VkRingCreateInfoMESA INFO-STRUCT body — the field-encode order
//     from Mesa venus-protocol vn_encode_VkRingCreateInfoMESA_self.
//
// WHAT THIS PACKAGE DOES NOT — AND CANNOT — PROVE WITHOUT A HOST:
//
//   - That the host renderer actually advances `head` as it consumes
//     (here a *test* fake consumer does; a real one is the renderer).
//   - The `status` word's idle-bit semantics and the vkNotifyRingMESA
//     wakeup handshake (the consumer sets an idle bit; the producer must
//     notify to wake it). vn_ring.c reads VK_RING_STATUS_IDLE_BIT_MESA from
//     a venus-protocol generated header whose numeric value is host-defined
//     and NOT reproduced here; see Status / NotifyNeeded below.
//   - The COMMAND FRAMING that wraps the VkRingCreateInfoMESA body (the
//     vn_cs command header: command_size, command_type opcode, cmd_flags,
//     reply seqno) — that is part of the host command stream the renderer
//     decodes and is deliberately left to the encoder/transport layer.
//
// In short: the layout + the producer loop + the info-struct bytes are
// real; the LIVENESS of head/status is the renderer's job and is exactly
// the "real unknown" the venus README calls out.
//
// References:
//
//   - Mesa src/virtio/vulkan/vn_ring.c:
//     struct layout { alignas(64) uint32_t head; alignas(64) uint32_t
//     tail; alignas(64) uint32_t status; alignas(64) uint8_t buffer[]; };
//     layout->head_offset/tail_offset/status_offset/buffer_offset =
//     offsetof(...); extra_offset = buffer_offset + buffer_size.
//     Producer: write to buffer at (cur & buffer_mask); tail store is
//     atomic_store_explicit(..., memory_order_release); head load is
//     atomic_load_explicit(..., memory_order_acquire); busy-wait on full.
//   - Mesa venus-protocol vn_protocol_driver_transport.h:
//     vn_encode_VkRingCreateInfoMESA_self field order (below).
package ring

import (
	"encoding/binary"
	"errors"
)

// --- Shared-memory layout (vn_ring.c: struct layout) ------------------
//
// Each control word sits on its own 64-byte cache line (the C struct uses
// `alignas(64)` on head/tail/status, and `buffer[]` follows aligned to 64).
// This yields the fixed offsets below — the layout the prompt and the venus
// README state (head@0 / tail@64 / status@128 / buffer@192).
const (
	// CacheLine is the alignment Mesa applies to each control word
	// (alignas(64)); it is also the spacing between head, tail and status.
	CacheLine = 64

	// HeadOffset is offsetof(struct layout, head) = 0. The head word is
	// CONSUMER-OWNED: the host renderer stores how far it has consumed.
	HeadOffset = 0
	// TailOffset is offsetof(struct layout, tail) = 64. The tail word is
	// PRODUCER-OWNED: the guest stores how far it has produced.
	TailOffset = CacheLine
	// StatusOffset is offsetof(struct layout, status) = 128. The status
	// word is CONSUMER-OWNED (idle/notify state).
	StatusOffset = 2 * CacheLine
	// BufferOffset is offsetof(struct layout, buffer) = 192. The command
	// byte buffer begins here.
	BufferOffset = 3 * CacheLine
)

// ControlWord is the on-the-wire width of head/tail/status (uint32_t).
const ControlWord = 4

// ShmemSize returns the total shared-memory size for a ring whose command
// buffer is bufferSize bytes and whose extra (reply) region is extraSize
// bytes, per vn_ring.c: extra_offset = buffer_offset + buffer_size;
// shmem_size = extra_offset + extra_size.
func ShmemSize(bufferSize, extraSize int) int {
	return BufferOffset + bufferSize + extraSize
}

// ExtraOffset returns offsetof of the extra region: buffer_offset +
// buffer_size (vn_ring.c).
func ExtraOffset(bufferSize int) int { return BufferOffset + bufferSize }

// isPow2 reports whether n is a power of two (>0). The command buffer size
// MUST be a power of two because the producer maps the monotonic write
// cursor to a buffer offset with a bitmask: offset = cur & (buffer_size-1)
// (vn_ring.c vn_ring_write_buffer).
func isPow2(n int) bool { return n > 0 && n&(n-1) == 0 }

// --- VkRingCreateInfoMESA info-struct body ----------------------------

// CreateInfo holds the VkRingCreateInfoMESA fields the guest sends to the
// host to stand up a ring. Field NAMES + ORDER are transcribed from Mesa
// vn_encode_VkRingCreateInfoMESA_self; sType/pNext are intentionally absent
// (the encoder skips them: "/* skip val->{sType,pNext} */"). All size_t
// fields encode as 8-byte LE (the venus wire treats size_t as 64-bit).
//
//	VkFlags  flags          (u32)
//	uint32_t resourceId     (u32)
//	size_t   offset         (u64) — byte offset of the layout within the blob
//	size_t   size           (u64) — total shmem size (== ShmemSize)
//	uint64_t idleTimeout    (u64)
//	size_t   headOffset     (u64)
//	size_t   tailOffset     (u64)
//	size_t   statusOffset   (u64)
//	size_t   bufferOffset   (u64)
//	size_t   bufferSize     (u64)
//	size_t   extraOffset    (u64)
//	size_t   extraSize      (u64)
type CreateInfo struct {
	Flags        uint32
	ResourceID   uint32
	Offset       uint64
	Size         uint64
	IdleTimeout  uint64
	HeadOffset   uint64
	TailOffset   uint64
	StatusOffset uint64
	BufferOffset uint64
	BufferSize   uint64
	ExtraOffset  uint64
	ExtraSize    uint64
}

// CreateInfoBodySize is the encoded size of the VkRingCreateInfoMESA body:
// two u32 (flags, resourceId) + ten u64 = 8 + 80 = 88 bytes.
const CreateInfoBodySize = 4 + 4 + 10*8

// EncodeCreateInfo serialises the VkRingCreateInfoMESA body in the exact
// field order of vn_encode_VkRingCreateInfoMESA_self. This is the INFO
// STRUCT only; the surrounding vn_cs command header (command_size /
// command_type / cmd_flags) is host-handshake framing emitted elsewhere and
// deliberately NOT added here (see package doc).
func EncodeCreateInfo(ci CreateInfo) []byte {
	b := make([]byte, CreateInfoBodySize)
	binary.LittleEndian.PutUint32(b[0:4], ci.Flags)
	binary.LittleEndian.PutUint32(b[4:8], ci.ResourceID)
	off := 8
	for _, v := range []uint64{
		ci.Offset, ci.Size, ci.IdleTimeout,
		ci.HeadOffset, ci.TailOffset, ci.StatusOffset,
		ci.BufferOffset, ci.BufferSize, ci.ExtraOffset, ci.ExtraSize,
	} {
		binary.LittleEndian.PutUint64(b[off:off+8], v)
		off += 8
	}
	return b
}

// CreateInfoForLayout builds a CreateInfo whose offset fields match the
// fixed vn_ring.c layout for a ring with the given resource id, command
// buffer size (power of two) and extra-region size, mapped at blob offset
// 0. idleTimeout is passed through. This is the canonical info a guest
// would send after RESOURCE_CREATE_BLOB+MAP_BLOB of the ring shmem.
func CreateInfoForLayout(resourceID uint32, bufferSize, extraSize int, idleTimeout uint64) (CreateInfo, error) {
	if !isPow2(bufferSize) {
		return CreateInfo{}, ErrBufferSizeNotPow2
	}
	if extraSize < 0 {
		return CreateInfo{}, ErrBadExtraSize
	}
	return CreateInfo{
		ResourceID:   resourceID,
		Offset:       0,
		Size:         uint64(ShmemSize(bufferSize, extraSize)),
		IdleTimeout:  idleTimeout,
		HeadOffset:   HeadOffset,
		TailOffset:   TailOffset,
		StatusOffset: StatusOffset,
		BufferOffset: BufferOffset,
		BufferSize:   uint64(bufferSize),
		ExtraOffset:  uint64(ExtraOffset(bufferSize)),
		ExtraSize:    uint64(extraSize),
	}, nil
}

// --- Pure-Go in-memory ring (producer + test consumer) ----------------

// Ring is a pure-Go model of the Venus command ring over a single backing
// byte slice laid out exactly per vn_ring.c. It models the PRODUCER side
// (the guest): Submit writes command bytes into the buffer at the masked
// cursor and advances tail. The head word is CONSUMER-owned; in production
// the host renderer advances it, and here AdvanceHead lets a test fake
// consumer do so. Concurrency note: this is a single-threaded model for
// offline testing — the real ring relies on release/acquire ordering
// across the guest/host boundary, which is NOT exercised here (and cannot
// be without a real consumer).
type Ring struct {
	mem        []byte
	bufferSize int
	bufferMask uint32
	extraSize  int
	cur        uint32 // monotonic producer write position (mirrors vn_ring.cur)
}

// New creates a Ring with a bufferSize-byte command buffer (power of two)
// and an extraSize-byte reply region, allocating its own backing slice
// sized to ShmemSize. head/tail/status start at 0.
func New(bufferSize, extraSize int) (*Ring, error) {
	if !isPow2(bufferSize) {
		return nil, ErrBufferSizeNotPow2
	}
	if extraSize < 0 {
		return nil, ErrBadExtraSize
	}
	return &Ring{
		mem:        make([]byte, ShmemSize(bufferSize, extraSize)),
		bufferSize: bufferSize,
		bufferMask: uint32(bufferSize - 1),
		extraSize:  extraSize,
	}, nil
}

// Mem exposes the backing shared-memory slice (read-only intent) for
// inspection in tests / wiring to a mapped blob.
func (r *Ring) Mem() []byte { return r.mem }

// BufferSize / ExtraSize return the configured region sizes.
func (r *Ring) BufferSize() int { return r.bufferSize }
func (r *Ring) ExtraSize() int  { return r.extraSize }

// Head returns the consumer-owned head word (acquire-load equivalent):
// how many bytes the consumer has consumed (monotonic, masked the same way
// as the producer's tail). In production this is written by the host.
func (r *Ring) Head() uint32 {
	return binary.LittleEndian.Uint32(r.mem[HeadOffset : HeadOffset+ControlWord])
}

// Tail returns the producer-owned tail word: how many bytes the producer
// has produced (== cur). This is what Submit release-stores.
func (r *Ring) Tail() uint32 {
	return binary.LittleEndian.Uint32(r.mem[TailOffset : TailOffset+ControlWord])
}

// Status returns the consumer-owned status word. Its bit semantics
// (notably the idle bit) are host-defined; see NotifyNeeded.
func (r *Ring) Status() uint32 {
	return binary.LittleEndian.Uint32(r.mem[StatusOffset : StatusOffset+ControlWord])
}

// free returns the number of buffer bytes the producer may write without
// overrunning the consumer: bufferSize - (cur - head). vn_ring.c keeps the
// invariant cur - head <= buffer_size.
func (r *Ring) free() uint32 {
	return uint32(r.bufferSize) - (r.cur - r.Head())
}

// Submit writes len(cmd) command bytes into the ring buffer at the masked
// write cursor and release-stores the new tail. It returns ErrCommandTooBig
// if cmd is larger than the whole buffer, and ErrRingFull if there is not
// currently enough free space (the caller would busy-poll head and retry —
// see SubmitBlocking).
//
// The write handles buffer wrap: when (cur & mask)+len exceeds bufferSize
// the bytes split across the end and start of the buffer region, exactly as
// the ring's power-of-two masking dictates.
func (r *Ring) Submit(cmd []byte) error {
	n := uint32(len(cmd))
	if int(n) > r.bufferSize {
		return ErrCommandTooBig
	}
	if r.free() < n {
		return ErrRingFull
	}
	start := r.cur & r.bufferMask
	bufBase := BufferOffset
	if start+n <= uint32(r.bufferSize) {
		copy(r.mem[bufBase+int(start):bufBase+int(start)+int(n)], cmd)
	} else {
		// Wrap: first chunk to end of buffer, remainder at buffer start.
		first := uint32(r.bufferSize) - start
		copy(r.mem[bufBase+int(start):bufBase+r.bufferSize], cmd[:first])
		copy(r.mem[bufBase:bufBase+int(n-first)], cmd[first:])
	}
	r.cur += n
	// Release-store the monotonic tail (memory_order_release in vn_ring.c).
	binary.LittleEndian.PutUint32(r.mem[TailOffset:TailOffset+ControlWord], r.cur)
	return nil
}

// Consumer is a test/fake consumer interface: in production the host
// renderer plays this role. It is provided so SubmitBlocking's busy-poll
// loop can be exercised deterministically offline.
type Consumer interface {
	// Consume is invoked when the producer is blocked on a full ring; an
	// implementation advances head (via Ring.AdvanceHead) to free space, or
	// does nothing to keep the ring full.
	Consume(r *Ring)
}

// AdvanceHead moves the consumer-owned head word forward by n bytes
// (release-store equivalent on the consumer side). It models the host
// renderer finishing consumption of n bytes. It returns ErrHeadOverrun if
// advancing would move head past the producer's tail (an illegal state).
func (r *Ring) AdvanceHead(n uint32) error {
	newHead := r.Head() + n
	if newHead > r.cur {
		return ErrHeadOverrun
	}
	binary.LittleEndian.PutUint32(r.mem[HeadOffset:HeadOffset+ControlWord], newHead)
	return nil
}

// SetStatus sets the consumer-owned status word (used by a test consumer to
// model the host setting/clearing the idle bit). The bit VALUES are
// host-defined and not interpreted by this package.
func (r *Ring) SetStatus(v uint32) {
	binary.LittleEndian.PutUint32(r.mem[StatusOffset:StatusOffset+ControlWord], v)
}

// NotifyNeeded reports whether the producer should send a vkNotifyRingMESA
// wakeup, given the host-defined idle-bit mask. It returns true iff
// (status & idleBit) != 0. The numeric idle-bit value is NOT known offline
// (it lives in a venus-protocol generated header and is host-defined), so
// the caller MUST supply it; this function only encodes the check shape
// from vn_ring.c (`if (status & VK_RING_STATUS_IDLE_BIT_MESA) ...`).
func (r *Ring) NotifyNeeded(idleBit uint32) bool {
	return r.Status()&idleBit != 0
}

// SubmitBlocking submits cmd, busy-polling a fake Consumer to free space
// when the ring is full — modelling vn_ring.c's submit loop that spins
// (vn_relax) reloading head until there is room. maxSpins bounds the loop so
// a consumer that never advances head surfaces ErrRingStalled instead of
// spinning forever. With a real host consumer there is no such bound; this
// bound exists only to make the offline model terminate and testable.
func (r *Ring) SubmitBlocking(cmd []byte, c Consumer, maxSpins int) error {
	for spin := 0; ; spin++ {
		err := r.Submit(cmd)
		if err == nil {
			return nil
		}
		// ErrCommandTooBig (command exceeds the whole buffer) is permanent —
		// no amount of consumer progress frees enough space, so return it.
		if !errors.Is(err, ErrRingFull) {
			return err
		}
		if spin >= maxSpins {
			return ErrRingStalled
		}
		c.Consume(r) // host renderer (or test fake) advances head
	}
}

// --- errors -----------------------------------------------------------

var (
	// ErrBufferSizeNotPow2 — the command buffer size must be a power of two
	// (the producer masks the write cursor with buffer_size-1).
	ErrBufferSizeNotPow2 = errors.New("go-virtio/venus/ring: buffer size must be a power of two")
	// ErrBadExtraSize — extra-region size must be non-negative.
	ErrBadExtraSize = errors.New("go-virtio/venus/ring: extra size must be non-negative")
	// ErrCommandTooBig — a single command exceeds the whole ring buffer.
	ErrCommandTooBig = errors.New("go-virtio/venus/ring: command larger than ring buffer")
	// ErrRingFull — not enough free space right now (caller retries after
	// the consumer advances head).
	ErrRingFull = errors.New("go-virtio/venus/ring: ring full (insufficient free space)")
	// ErrHeadOverrun — advancing head past tail (illegal consumer state).
	ErrHeadOverrun = errors.New("go-virtio/venus/ring: head advanced past tail")
	// ErrRingStalled — the offline busy-poll budget elapsed without the
	// fake consumer freeing space (model-only; no real-host analogue).
	ErrRingStalled = errors.New("go-virtio/venus/ring: ring stalled (consumer did not advance head)")
)
