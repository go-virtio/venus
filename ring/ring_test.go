package ring

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// --- layout offsets (vn_ring.c: alignas(64) head/tail/status) ---------

func TestLayoutOffsets(t *testing.T) {
	if CacheLine != 64 {
		t.Errorf("CacheLine: got %d, want 64", CacheLine)
	}
	if HeadOffset != 0 {
		t.Errorf("HeadOffset: got %d, want 0", HeadOffset)
	}
	if TailOffset != 64 {
		t.Errorf("TailOffset: got %d, want 64", TailOffset)
	}
	if StatusOffset != 128 {
		t.Errorf("StatusOffset: got %d, want 128", StatusOffset)
	}
	if BufferOffset != 192 {
		t.Errorf("BufferOffset: got %d, want 192", BufferOffset)
	}
	if ControlWord != 4 {
		t.Errorf("ControlWord: got %d, want 4", ControlWord)
	}
}

func TestShmemSizeAndExtraOffset(t *testing.T) {
	if got := ShmemSize(256, 64); got != 192+256+64 {
		t.Errorf("ShmemSize: got %d, want %d", got, 192+256+64)
	}
	if got := ExtraOffset(256); got != 192+256 {
		t.Errorf("ExtraOffset: got %d, want %d", got, 192+256)
	}
}

// --- VkRingCreateInfoMESA body encoding -------------------------------

func TestEncodeCreateInfo_FieldOrder(t *testing.T) {
	ci := CreateInfo{
		Flags:        0x11,
		ResourceID:   0x22,
		Offset:       0x0100,
		Size:         0x0200,
		IdleTimeout:  0x0300,
		HeadOffset:   0,
		TailOffset:   64,
		StatusOffset: 128,
		BufferOffset: 192,
		BufferSize:   256,
		ExtraOffset:  192 + 256,
		ExtraSize:    64,
	}
	b := EncodeCreateInfo(ci)
	if len(b) != CreateInfoBodySize || len(b) != 88 {
		t.Fatalf("length: got %d, want 88", len(b))
	}
	if binary.LittleEndian.Uint32(b[0:4]) != 0x11 {
		t.Errorf("flags@0: got 0x%x", binary.LittleEndian.Uint32(b[0:4]))
	}
	if binary.LittleEndian.Uint32(b[4:8]) != 0x22 {
		t.Errorf("resourceId@4: got 0x%x", binary.LittleEndian.Uint32(b[4:8]))
	}
	// Ten u64 in declared order starting at offset 8.
	wantU64 := []uint64{0x0100, 0x0200, 0x0300, 0, 64, 128, 192, 256, 192 + 256, 64}
	for i, w := range wantU64 {
		off := 8 + i*8
		if got := binary.LittleEndian.Uint64(b[off : off+8]); got != w {
			t.Errorf("u64 field %d @%d: got 0x%x, want 0x%x", i, off, got, w)
		}
	}
}

func TestCreateInfoForLayout(t *testing.T) {
	ci, err := CreateInfoForLayout(5, 1024, 256, 1_000_000)
	if err != nil {
		t.Fatalf("CreateInfoForLayout: %v", err)
	}
	if ci.ResourceID != 5 {
		t.Errorf("ResourceID: got %d, want 5", ci.ResourceID)
	}
	if ci.HeadOffset != 0 || ci.TailOffset != 64 || ci.StatusOffset != 128 || ci.BufferOffset != 192 {
		t.Errorf("offsets wrong: %+v", ci)
	}
	if ci.BufferSize != 1024 || ci.ExtraSize != 256 {
		t.Errorf("sizes wrong: %+v", ci)
	}
	if ci.ExtraOffset != 192+1024 {
		t.Errorf("ExtraOffset: got %d, want %d", ci.ExtraOffset, 192+1024)
	}
	if ci.Size != uint64(ShmemSize(1024, 256)) {
		t.Errorf("Size: got %d, want %d", ci.Size, ShmemSize(1024, 256))
	}
	if ci.IdleTimeout != 1_000_000 {
		t.Errorf("IdleTimeout: got %d", ci.IdleTimeout)
	}
}

func TestCreateInfoForLayout_Errors(t *testing.T) {
	if _, err := CreateInfoForLayout(1, 1000, 0, 0); !errors.Is(err, ErrBufferSizeNotPow2) {
		t.Errorf("non-pow2 buffer: got %v, want ErrBufferSizeNotPow2", err)
	}
	if _, err := CreateInfoForLayout(1, 1024, -1, 0); !errors.Is(err, ErrBadExtraSize) {
		t.Errorf("negative extra: got %v, want ErrBadExtraSize", err)
	}
}

// --- Ring construction ------------------------------------------------

func TestNew_Errors(t *testing.T) {
	if _, err := New(0, 0); !errors.Is(err, ErrBufferSizeNotPow2) {
		t.Errorf("New(0): got %v, want ErrBufferSizeNotPow2", err)
	}
	if _, err := New(100, 0); !errors.Is(err, ErrBufferSizeNotPow2) {
		t.Errorf("New(100): got %v, want ErrBufferSizeNotPow2", err)
	}
	if _, err := New(64, -1); !errors.Is(err, ErrBadExtraSize) {
		t.Errorf("New(64,-1): got %v, want ErrBadExtraSize", err)
	}
}

func TestNew_LayoutAndAccessors(t *testing.T) {
	r, err := New(128, 32)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if len(r.Mem()) != ShmemSize(128, 32) {
		t.Errorf("Mem size: got %d, want %d", len(r.Mem()), ShmemSize(128, 32))
	}
	if r.BufferSize() != 128 || r.ExtraSize() != 32 {
		t.Errorf("sizes: got buf=%d extra=%d", r.BufferSize(), r.ExtraSize())
	}
	if r.Head() != 0 || r.Tail() != 0 || r.Status() != 0 {
		t.Errorf("initial head/tail/status not zero: %d/%d/%d", r.Head(), r.Tail(), r.Status())
	}
}

// --- Submit: write placement + tail advance ---------------------------

func TestSubmit_WritesAtBufferOffsetAndAdvancesTail(t *testing.T) {
	r, _ := New(64, 0)
	cmd := []byte{1, 2, 3, 4, 5}
	if err := r.Submit(cmd); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if r.Tail() != 5 {
		t.Errorf("tail after submit: got %d, want 5", r.Tail())
	}
	// Bytes must land at BufferOffset (192).
	if !bytes.Equal(r.Mem()[BufferOffset:BufferOffset+5], cmd) {
		t.Errorf("buffer bytes: got %v, want %v", r.Mem()[BufferOffset:BufferOffset+5], cmd)
	}
	// A second submit continues at cur=5.
	if err := r.Submit([]byte{6, 7}); err != nil {
		t.Fatalf("Submit 2: %v", err)
	}
	if r.Tail() != 7 {
		t.Errorf("tail after 2nd submit: got %d, want 7", r.Tail())
	}
	if !bytes.Equal(r.Mem()[BufferOffset+5:BufferOffset+7], []byte{6, 7}) {
		t.Errorf("second chunk misplaced: %v", r.Mem()[BufferOffset+5:BufferOffset+7])
	}
}

func TestSubmit_CommandTooBig(t *testing.T) {
	r, _ := New(8, 0)
	if err := r.Submit(make([]byte, 9)); !errors.Is(err, ErrCommandTooBig) {
		t.Errorf("got %v, want ErrCommandTooBig", err)
	}
}

func TestSubmit_RingFull(t *testing.T) {
	r, _ := New(8, 0)
	if err := r.Submit(make([]byte, 8)); err != nil { // fills the ring
		t.Fatalf("fill: %v", err)
	}
	if r.free() != 0 {
		t.Fatalf("free after fill: got %d, want 0", r.free())
	}
	if err := r.Submit([]byte{1}); !errors.Is(err, ErrRingFull) {
		t.Errorf("got %v, want ErrRingFull", err)
	}
}

// --- Submit: wrap-around (power-of-two mask) --------------------------

func TestSubmit_Wraps(t *testing.T) {
	r, _ := New(8, 0)
	// Produce 6, consume 6 -> cur=6, head=6, free=8, masked write start = 6.
	if err := r.Submit(make([]byte, 6)); err != nil {
		t.Fatalf("submit 6: %v", err)
	}
	if err := r.AdvanceHead(6); err != nil {
		t.Fatalf("advance head 6: %v", err)
	}
	// Now submit 5 bytes: starts at offset 6, wraps: 2 at end + 3 at start.
	cmd := []byte{0xA, 0xB, 0xC, 0xD, 0xE}
	if err := r.Submit(cmd); err != nil {
		t.Fatalf("wrap submit: %v", err)
	}
	if r.Tail() != 11 {
		t.Errorf("tail after wrap: got %d, want 11", r.Tail())
	}
	// Bytes 0,1 of cmd at buffer offset 6,7; bytes 2,3,4 at offset 0,1,2.
	if !bytes.Equal(r.Mem()[BufferOffset+6:BufferOffset+8], cmd[:2]) {
		t.Errorf("wrap tail chunk: got %v, want %v", r.Mem()[BufferOffset+6:BufferOffset+8], cmd[:2])
	}
	if !bytes.Equal(r.Mem()[BufferOffset:BufferOffset+3], cmd[2:]) {
		t.Errorf("wrap head chunk: got %v, want %v", r.Mem()[BufferOffset:BufferOffset+3], cmd[2:])
	}
}

// --- AdvanceHead ------------------------------------------------------

func TestAdvanceHead_Overrun(t *testing.T) {
	r, _ := New(16, 0)
	_ = r.Submit(make([]byte, 4)) // cur=4
	if err := r.AdvanceHead(5); !errors.Is(err, ErrHeadOverrun) {
		t.Errorf("got %v, want ErrHeadOverrun", err)
	}
	if err := r.AdvanceHead(4); err != nil {
		t.Errorf("advance to tail should be legal: %v", err)
	}
	if r.Head() != 4 {
		t.Errorf("head: got %d, want 4", r.Head())
	}
}

// --- status / notify --------------------------------------------------

func TestStatusAndNotifyNeeded(t *testing.T) {
	r, _ := New(16, 0)
	const idleBit uint32 = 1 << 0 // host-defined; arbitrary here
	if r.NotifyNeeded(idleBit) {
		t.Error("idle not set yet -> NotifyNeeded should be false")
	}
	r.SetStatus(idleBit)
	if r.Status() != idleBit {
		t.Errorf("Status: got 0x%x, want 0x%x", r.Status(), idleBit)
	}
	if !r.NotifyNeeded(idleBit) {
		t.Error("idle set -> NotifyNeeded should be true")
	}
	// A different bit set, idle bit clear -> no notify.
	r.SetStatus(1 << 2)
	if r.NotifyNeeded(idleBit) {
		t.Error("idle bit clear -> NotifyNeeded should be false")
	}
}

// --- SubmitBlocking busy-poll loop ------------------------------------

// freeingConsumer advances head by `step` each time it is polled, modelling
// a host renderer draining the ring.
type freeingConsumer struct{ step uint32 }

func (c *freeingConsumer) Consume(r *Ring) { _ = r.AdvanceHead(c.step) }

// idleConsumer never advances head (models a stuck/absent consumer).
type idleConsumer struct{}

func (idleConsumer) Consume(*Ring) {}

func TestSubmitBlocking_SucceedsAfterConsumerFrees(t *testing.T) {
	r, _ := New(8, 0)
	_ = r.Submit(make([]byte, 8)) // full; head=0
	c := &freeingConsumer{step: 4}
	// Need 4 free; one Consume (head+=4) frees enough, then submit succeeds.
	if err := r.SubmitBlocking(make([]byte, 4), c, 10); err != nil {
		t.Fatalf("SubmitBlocking: %v", err)
	}
	if r.Tail() != 12 {
		t.Errorf("tail: got %d, want 12", r.Tail())
	}
}

func TestSubmitBlocking_ImmediateNoSpin(t *testing.T) {
	r, _ := New(16, 0)
	if err := r.SubmitBlocking([]byte{1, 2, 3}, idleConsumer{}, 0); err != nil {
		t.Fatalf("SubmitBlocking with space available: %v", err)
	}
}

func TestSubmitBlocking_Stalled(t *testing.T) {
	r, _ := New(8, 0)
	_ = r.Submit(make([]byte, 8)) // full; consumer never advances head
	if err := r.SubmitBlocking([]byte{1}, idleConsumer{}, 3); !errors.Is(err, ErrRingStalled) {
		t.Errorf("got %v, want ErrRingStalled", err)
	}
}

func TestSubmitBlocking_CommandTooBig(t *testing.T) {
	r, _ := New(8, 0)
	if err := r.SubmitBlocking(make([]byte, 9), idleConsumer{}, 5); !errors.Is(err, ErrCommandTooBig) {
		t.Errorf("got %v, want ErrCommandTooBig", err)
	}
}
