package gen

import (
	"bytes"
	"strings"
	"testing"
)

// rbStructs/rbCommands mirror cmd/vkgen's readback proof set so the gen package
// drives every new emit path (partial encoders, the five new reply-decoder
// shapes, the fixed-array-of-struct decode, the struct-field-counted handle
// array, and the by-value uint64/VkBool32/VkDeviceSize params) directly.
var (
	rbDecodeStructs = []string{
		"VkMemoryRequirements", "VkPhysicalDeviceProperties",
		"VkPhysicalDeviceMemoryProperties", "VkQueueFamilyProperties",
	}
	rbPartialStructs = []string{
		"VkMemoryRequirements", "VkPhysicalDeviceMemoryProperties", "VkQueueFamilyProperties",
	}
)

// TestEmitReadbackFullSet exercises the whole new emit surface over the curated
// registry and asserts the generated function signatures/bodies that mark each
// new Mesa shape.
func TestEmitReadbackFullSet(t *testing.T) {
	reg := loadFull(t)
	src, err := NewEmitter(reg,
		[]string{"VkCommandBufferBeginInfo"},
		[]string{
			"vkGetPhysicalDeviceMemoryProperties",
			"vkGetPhysicalDeviceQueueFamilyProperties",
			"vkGetDeviceQueue",
			"vkGetImageMemoryRequirements",
			"vkBindImageMemory",
			"vkAllocateCommandBuffers",
			"vkBeginCommandBuffer",
			"vkEndCommandBuffer",
			"vkQueueWaitIdle",
			"vkWaitForFences",
		}).
		WithDecodeStructs(rbDecodeStructs).
		WithPartialStructs(rbPartialStructs).
		WithResultReplies([]string{"vkBindImageMemory", "vkBeginCommandBuffer", "vkEndCommandBuffer", "vkQueueWaitIdle", "vkWaitForFences"}).
		WithVoidHandleReplies([]string{"vkGetDeviceQueue"}).
		WithStructReplies([]string{"vkGetImageMemoryRequirements", "vkGetPhysicalDeviceMemoryProperties"}).
		WithCountStructArrayReplies([]string{"vkGetPhysicalDeviceQueueFamilyProperties"}).
		WithCountHandleArrayStructReplies([]string{"vkAllocateCommandBuffers"}).
		Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		// by-value scalar params
		"timeout uint64",
		"enc.EncodeUint64(timeout)",
		"waitAll bool",
		"enc.EncodeBool32(waitAll)",
		"memoryOffset uint64",
		"enc.EncodeDeviceSize(memoryOffset)",
		// partial encoders
		"func EncodeVkMemoryRequirementsPartial(",
		"func EncodeVkPhysicalDeviceMemoryPropertiesPartial(",
		"func EncodeVkMemoryTypePartial(",                   // nested partial, pulled in transitively
		"func EncodeVkExtent3DPartial(",                     // nested-by-value partial
		"EncodeVkMemoryTypePartial(enc, &v.MemoryTypes[i])", // fixed-array-of-struct partial
		"EncodeVkExtent3DPartial(enc, &v.MinImageTransferGranularity)",
		// partial used in command bodies
		"EncodeVkMemoryRequirementsPartial(enc, pMemoryRequirements)",
		"EncodeVkPhysicalDeviceMemoryPropertiesPartial(enc, pMemoryProperties)",
		"EncodeVkQueueFamilyPropertiesPartial(enc, &pQueueFamilyProperties[i])",
		// struct-field-counted handle array param
		"n = pAllocateInfo.CommandBufferCount",
		"for i := uint32(0); i < n; i++ {",
		// fixed-array-of-struct decode
		"DecodeVkMemoryType(dec, &v.MemoryTypes[i])",
		"DecodeVkMemoryHeap(dec, &v.MemoryHeaps[i])",
		// the five new reply-decoder shapes
		"func Decode_vkBindImageMemory_reply(dec *vncs.Decoder) (cmdType int32, result int32)",
		"func Decode_vkGetDeviceQueue_reply(dec *vncs.Decoder) (cmdType int32, pQueue uint64, ok bool)",
		"func Decode_vkGetImageMemoryRequirements_reply(dec *vncs.Decoder, pMemoryRequirements *VkMemoryRequirements) (cmdType int32, ok bool)",
		"func Decode_vkGetPhysicalDeviceQueueFamilyProperties_reply(",
		"func Decode_vkAllocateCommandBuffers_reply(dec *vncs.Decoder, maxCount uint32)",
		"const VkQueueWaitIdleReplyCmdType int32 = 19",
		"const VkGetPhysicalDeviceMemoryPropertiesReplyCmdType int32 = 8",
		"const VkGetPhysicalDeviceQueueFamilyPropertiesReplyCmdType int32 = 7",
		// fixed-array-of-struct input field
		"MemoryTypes [32]VkMemoryType",
		"MinImageTransferGranularity VkExtent3D",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("readback source missing %q", want)
		}
	}
}

// TestGoTypeOfFixedStructArray covers the fixed-array-of-struct goType mapping.
func TestGoTypeOfFixedStructArray(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{"VkMemoryType": {}},
		EnumValues: map[string]string{"VK_MAX_MEMORY_TYPES": "32"},
	}
	if got := goTypeOf(reg, &Member{Type: "VkMemoryType", FixedArrayLen: "VK_MAX_MEMORY_TYPES"}); got != "[32]VkMemoryType" {
		t.Errorf("fixed struct array = %q want [32]VkMemoryType", got)
	}
}

// TestSplitStructFieldLen unit-covers the "struct->field" len splitter.
func TestSplitStructFieldLen(t *testing.T) {
	s, f := splitStructFieldLen("pAllocateInfo->commandBufferCount")
	if s != "pAllocateInfo" || f != "commandBufferCount" {
		t.Errorf("splitStructFieldLen = (%q,%q)", s, f)
	}
}

// TestEmitReadbackValidationErrors covers the Generate-time validation branches
// for the new option sets and the per-emitter shape errors.
func TestEmitReadbackValidationErrors(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}

	// Unknown partial struct -> Generate error.
	if _, err := NewEmitter(reg, nil, nil).WithPartialStructs([]string{"VkGhost"}).Generate("proof"); err == nil {
		t.Error("expected error for unknown partial struct")
	}
	// Unknown command in each new reply set -> Generate error.
	for _, with := range []func(*Emitter) *Emitter{
		func(e *Emitter) *Emitter { return e.WithResultReplies([]string{"vkGhost"}) },
		func(e *Emitter) *Emitter { return e.WithVoidHandleReplies([]string{"vkGhost"}) },
		func(e *Emitter) *Emitter { return e.WithStructReplies([]string{"vkGhost"}) },
		func(e *Emitter) *Emitter { return e.WithCountStructArrayReplies([]string{"vkGhost"}) },
		func(e *Emitter) *Emitter { return e.WithCountHandleArrayStructReplies([]string{"vkGhost"}) },
	} {
		if _, err := with(NewEmitter(reg, nil, nil)).Generate("proof"); err == nil {
			t.Error("expected error for unknown reply command")
		}
	}

	e := NewEmitter(reg, nil, nil)
	var b bytes.Buffer

	// replyHeader without an ordinal -> error (and so each emitter that calls it).
	noOrd := &Command{Name: "vkNoOrd", Params: nil}
	if _, err := e.replyHeader(&b, noOrd); err == nil {
		t.Error("expected replyHeader ordinal error")
	}
	for _, emit := range []func(*bytes.Buffer, *Command) error{
		e.emitResultReplyDecoder, e.emitVoidHandleReplyDecoder, e.emitStructReplyDecoder,
		e.emitCountStructArrayReplyDecoder, e.emitCountHandleArrayStructReplyDecoder,
	} {
		if err := emit(&b, noOrd); err == nil {
			t.Error("expected ordinal error from reply emitter")
		}
	}

	// Shape errors: an ordinal-bearing command lacking the required out-param.
	venusCommandTypeValues["vkShape"] = "950"
	defer delete(venusCommandTypeValues, "vkShape")
	shapeless := &Command{Name: "vkShape", Params: []*Param{{Name: "x", Type: "uint32_t"}}}
	if err := e.emitVoidHandleReplyDecoder(&b, shapeless); err == nil {
		t.Error("expected void-handle reply shape error")
	}
	if err := e.emitStructReplyDecoder(&b, shapeless); err == nil {
		t.Error("expected struct reply shape error")
	}
	if err := e.emitCountStructArrayReplyDecoder(&b, shapeless); err == nil {
		t.Error("expected count-struct-array reply shape error")
	}
	if err := e.emitCountHandleArrayStructReplyDecoder(&b, shapeless); err == nil {
		t.Error("expected struct-field handle-array reply shape error")
	}
}

// TestEmitPartialErrors covers the partial-encoder error branches and the
// nested-struct/fixed-array partial emit paths via a synthetic registry.
func TestEmitPartialErrors(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	e := NewEmitter(reg, nil, nil)
	var b bytes.Buffer

	// A fixed-array-of-struct member with an unknown length -> partial error.
	reg.Structs["VkInner"] = &Struct{Name: "VkInner", Members: []*Member{{Name: "v", Type: "uint32_t"}}}
	reg.Structs["VkBadPartial"] = &Struct{Name: "VkBadPartial", Members: []*Member{
		{Name: "arr", Type: "VkInner", FixedArrayLen: "VK_UNKNOWN"},
	}}
	if err := e.emitStructPartialEncoder(&b, reg.Structs["VkBadPartial"]); err == nil {
		t.Error("expected partial fixed-array unknown-length error")
	}
	// And via Generate (the partialNeeded loop).
	if _, err := NewEmitter(reg, nil, nil).WithPartialStructs([]string{"VkBadPartial"}).Generate("proof"); err == nil {
		t.Error("expected partial error through Generate")
	}

	// emitMemberPartial skips sType/pNext without writing.
	var sk bytes.Buffer
	if err := e.emitMemberPartial(&sk, "v", &Member{Name: "sType", IsSType: true}); err != nil || sk.Len() != 0 {
		t.Errorf("emitMemberPartial(sType) wrote %q err=%v", sk.String(), err)
	}
}

// TestGenerateReplyErrorPropagation covers the `return nil, err` propagation of
// each new reply-decoder loop in Generate. A command placed only in a reply set
// (not the commands set, so no encoder runs) with no VkCommandTypeEXT ordinal
// makes its reply emitter error from inside Generate.
func TestGenerateReplyErrorPropagation(t *testing.T) {
	for _, with := range []func(*Emitter) *Emitter{
		func(e *Emitter) *Emitter { return e.WithResultReplies([]string{"vkNoOrdReply"}) },
		func(e *Emitter) *Emitter { return e.WithVoidHandleReplies([]string{"vkNoOrdReply"}) },
		func(e *Emitter) *Emitter { return e.WithStructReplies([]string{"vkNoOrdReply"}) },
		func(e *Emitter) *Emitter { return e.WithCountStructArrayReplies([]string{"vkNoOrdReply"}) },
		func(e *Emitter) *Emitter { return e.WithCountHandleArrayStructReplies([]string{"vkNoOrdReply"}) },
	} {
		reg := &Registry{
			Structs:    map[string]*Struct{},
			Commands:   map[string]*Command{"vkNoOrdReply": {Name: "vkNoOrdReply"}},
			EnumValues: map[string]string{},
			EnumTypes:  map[string]bool{},
			Handles:    map[string]bool{},
			Unions:     map[string]bool{},
		}
		if _, err := with(NewEmitter(reg, nil, nil)).Generate("proof"); err == nil {
			t.Error("expected reply-decoder error through Generate")
		}
	}
}

// TestEmitPartialSkipAndSharedNested covers the sType/pNext skip in a partial
// encoder and the already-visited branch of neededPartialStructs (a nested
// struct shared by two members of the same parent).
func TestEmitPartialSkipAndSharedNested(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{"VK_STRUCTURE_TYPE_X": "9"},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	reg.Structs["VkShared"] = &Struct{Name: "VkShared", Members: []*Member{{Name: "v", Type: "uint32_t"}}}
	// A partial struct carrying sType+pNext (skipped) and TWO members of the same
	// nested struct (so neededPartialStructs visits VkShared once though it is
	// referenced twice).
	reg.Structs["VkPartialHdr"] = &Struct{Name: "VkPartialHdr", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "pNext", IsPNext: true},
		{Name: "a", Type: "VkShared"},
		{Name: "b", Type: "VkShared"},
	}}
	reg.structOrder = []string{"VkShared", "VkPartialHdr"}
	src, err := NewEmitter(reg, nil, nil).WithPartialStructs([]string{"VkPartialHdr"}).Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		"func EncodeVkSharedPartial(",
		"func EncodeVkPartialHdrPartial(",
		"EncodeVkSharedPartial(enc, &v.A)",
		"EncodeVkSharedPartial(enc, &v.B)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q\n%s", want, s)
		}
	}
	// VkShared must be emitted once (the shared-nested visit guard).
	if strings.Count(s, "func EncodeVkSharedPartial(") != 1 {
		t.Errorf("VkSharedPartial emitted %d times, want 1", strings.Count(s, "func EncodeVkSharedPartial("))
	}
}

// TestEmitDecodeFixedStructArrayErrors covers the multidim/unknown-length error
// branches of the fixed-array-of-struct DECODE path.
func TestEmitDecodeFixedStructArrayErrors(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	reg.Structs["VkElem"] = &Struct{Name: "VkElem", Members: []*Member{{Name: "v", Type: "uint32_t"}}}
	e := NewEmitter(reg, nil, nil)
	var b bytes.Buffer
	if err := e.emitMemberDecode(&b, "v", &Member{Name: "arr", Type: "VkElem", FixedArrayLen: "3[multidim]"}); err == nil {
		t.Error("expected multidim struct-array decode error")
	}
	if err := e.emitMemberDecode(&b, "v", &Member{Name: "arr", Type: "VkElem", FixedArrayLen: "VK_UNKNOWN"}); err == nil {
		t.Error("expected unknown-length struct-array decode error")
	}
}
