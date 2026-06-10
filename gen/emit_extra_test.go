package gen

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// fullProofSubset mirrors cmd/vkgen's proof set so the gen package drives
// every emit path (union, fixed arrays, enums, handles, counted arrays,
// reply decoders) directly.
var (
	fpStructs = []string{
		"VkInstanceCreateInfo", "VkDeviceCreateInfo", "VkImageCreateInfo",
		"VkMemoryAllocateInfo", "VkCommandPoolCreateInfo",
		"VkCommandBufferAllocateInfo", "VkImageSubresourceRange",
		"VkClearColorValue",
	}
	fpCommands = []string{
		"vkCreateInstance", "vkEnumeratePhysicalDevices", "vkCreateDevice",
		"vkCreateImage", "vkAllocateMemory", "vkCreateCommandPool",
		"vkCmdClearColorImage",
	}
	fpReplies = []string{
		"vkCreateInstance", "vkCreateDevice", "vkCreateImage",
		"vkAllocateMemory", "vkCreateCommandPool",
	}
)

func loadFull(t *testing.T) *Registry {
	t.Helper()
	data, err := os.ReadFile("testdata/vk_subset.xml")
	if err != nil {
		t.Fatalf("read testdata: %v", err)
	}
	reg, err := Parse(data)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return reg
}

// TestEmitFullProofSubset exercises the whole emit pipeline over the real
// curated registry, covering the union encoder, counted struct/scalar arrays,
// enums, handles, VkDeviceSize, and every reply decoder.
func TestEmitFullProofSubset(t *testing.T) {
	reg := loadFull(t)
	src, err := NewEmitter(reg, fpStructs, fpCommands).WithReplies(fpReplies).Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		"func EncodeVkExtent3D(",                                      // nested-by-value
		"func EncodeVkClearColorValue(",                               // union
		"enc.EncodeFloat32Array(v.Float32[:])",                        // union arm 0
		"enc.EncodeInt32Array(v.Int32[:])",                            // union arm 1
		"enc.EncodeUint32Array(v.Uint32[:])",                          // union arm 2
		"enc.EncodeDeviceSize(v.AllocationSize)",                      // VkDeviceSize
		"enc.EncodeInt32(v.ImageType) // enum VkImageType",            // enum member
		"enc.EncodeHandle(v.CommandPool) // handle VkCommandPool",     // handle member
		"enc.EncodeFloat32Array(v.PQueuePriorities)",                  // counted scalar array
		"EncodeVkDeviceQueueCreateInfo(enc, &v.PQueueCreateInfos[i])", // counted struct array
		"enc.EncodeHandle(physicalDevice) // handle VkPhysicalDevice", // by-value handle param
		"enc.EncodeInt32(imageLayout) // enum VkImageLayout",          // enum param
		"func Decode_vkCreateInstance_reply(",                         // reply decoder
		"const VkCreateDeviceReplyCmdType int32 = 11",                 // reply const
		"for i := range pPhysicalDevices",                             // counted handle array param
	} {
		if !strings.Contains(s, want) {
			t.Errorf("generated source missing %q", want)
		}
	}
}

// TestEmitPNextChainsAndNodes drives the pNext-chain encoders (VkSubmitInfo /
// VkImageMemoryBarrier), the extension-node self-encoder + constructor
// (VkProtectedSubmitInfo), the counted handle/VkFlags array members, and the
// vkQueueSubmit / vkCmdPipelineBarrier framing over the curated registry.
func TestEmitPNextChainsAndNodes(t *testing.T) {
	reg := loadFull(t)
	src, err := NewEmitter(reg,
		[]string{"VkSubmitInfo", "VkImageMemoryBarrier"},
		[]string{"vkQueueSubmit", "vkCmdPipelineBarrier"}).
		WithPNextChains([]string{"VkSubmitInfo", "VkImageMemoryBarrier"}).
		WithPNextNodes([]string{"VkProtectedSubmitInfo"}).
		Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		"PNext []vncs.PNextNode",                                                  // chain input field
		"enc.EncodePNextChain(v.PNext) // pNext extension chain",                  // chain call
		"enc.EncodeHandle(v.PWaitSemaphores[i])",                                  // counted handle array member
		"enc.EncodeFlags(v.PWaitDstStageMask[i])",                                 // counted VkFlags array member
		"func EncodeVkProtectedSubmitInfoSelf(",                                   // node self encoder
		"func VkProtectedSubmitInfoNode(v *VkProtectedSubmitInfo) vncs.PNextNode", // node ctor
		"SType: 1000145000",                                                       // node sType wired
		"func Encode_vkQueueSubmit(",                                              // submit framing
		"EncodeVkSubmitInfo(enc, &pSubmits[i])",                                   // counted struct array param
		"enc.EncodeHandle(fence) // handle VkFence",                               // by-value handle param
		"func Encode_vkCmdPipelineBarrier(",                                       // barrier framing
		"EncodeVkImageMemoryBarrier(enc, &pImageMemoryBarriers[i])",
		"EncodeVkMemoryBarrier(enc, &pMemoryBarriers[i])",
		"EncodeVkBufferMemoryBarrier(enc, &pBufferMemoryBarriers[i])",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("pNext/framing source missing %q", want)
		}
	}
}

// TestEmitPNextNodeErrors covers the validation branches of the pNext node
// emitter and the chain/node Generate validations.
func TestEmitPNextNodeErrors(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{"VK_STRUCTURE_TYPE_X": "9"},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	e := NewEmitter(reg, nil, nil)

	// Unknown pNext node struct -> Generate error.
	if _, err := NewEmitter(reg, nil, nil).WithPNextNodes([]string{"VkGhost"}).Generate("proof"); err == nil {
		t.Error("expected error for unknown pNext node struct")
	}

	// A pNext node with no sType -> error.
	reg.Structs["VkNoSTypeNode"] = &Struct{Name: "VkNoSTypeNode", Members: []*Member{{Name: "x", Type: "uint32_t"}}}
	var b bytes.Buffer
	if err := e.emitPNextNode(&b, reg.Structs["VkNoSTypeNode"]); err == nil {
		t.Error("expected error for sType-less pNext node")
	}

	// A pNext node whose sType token has no enum value -> error.
	reg.Structs["VkBadEnumNode"] = &Struct{Name: "VkBadEnumNode", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_UNKNOWN"},
		{Name: "a", Type: "uint32_t"},
	}}
	if err := e.emitPNextNode(&b, reg.Structs["VkBadEnumNode"]); err == nil {
		t.Error("expected error for pNext node with unknown sType value")
	}

	// A pNext node whose self member is unsupported -> error (emitMember fails).
	reg.Structs["VkBadSelfNode"] = &Struct{Name: "VkBadSelfNode", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "weird", Type: "VkExotic"},
	}}
	if err := e.emitPNextNode(&b, reg.Structs["VkBadSelfNode"]); err == nil {
		t.Error("expected error for pNext node with unsupported self member")
	}
	// And the same error propagating through Generate (the pNextNodes loop).
	if _, err := NewEmitter(reg, nil, nil).WithPNextNodes([]string{"VkBadSelfNode"}).Generate("proof"); err == nil {
		t.Error("expected pNext node error through Generate")
	}
}

// TestGoTypeOfCountedHandleAndFlags covers the counted handle/VkFlags array
// slice mappings added for VkSubmitInfo's members.
func TestGoTypeOfCountedHandleAndFlags(t *testing.T) {
	reg := &Registry{
		Structs: map[string]*Struct{},
		Handles: map[string]bool{"VkSemaphore": true},
	}
	if got := goTypeOf(reg, &Member{Type: "VkSemaphore", Pointer: true, Len: "n"}); got != "[]uint64" {
		t.Errorf("counted handle array = %q want []uint64", got)
	}
	if got := goTypeOf(reg, &Member{Type: "VkPipelineStageFlags", Pointer: true, Len: "n"}); got != "[]uint32" {
		t.Errorf("counted VkFlags array = %q want []uint32", got)
	}
}

// TestEmitDecodeAndCountArrayReply drives the count+array reply decoder, the
// returned-only struct decoders (including nested structs, fixed char/uint8/
// uint32/float arrays, size_t and enum members), exercising every new emit
// path over the curated registry.
func TestEmitDecodeAndCountArrayReply(t *testing.T) {
	reg := loadFull(t)
	src, err := NewEmitter(reg, fpStructs, fpCommands).
		WithReplies(fpReplies).
		WithCountArrayReplies([]string{"vkEnumeratePhysicalDevices"}).
		WithDecodeStructs([]string{"VkMemoryRequirements", "VkPhysicalDeviceProperties"}).
		Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		"func Decode_vkEnumeratePhysicalDevices_reply(", // count+array reply
		"const VkEnumeratePhysicalDevicesReplyCmdType int32 = 2",
		"dec.DecodeArraySizeUnchecked() // consume the array_size(0)",
		"pPhysicalDevices = make([]uint64, n)",
		"func DecodeVkMemoryRequirements(", // returned-only struct
		"v.Size = dec.DecodeDeviceSize()",
		"func DecodeVkPhysicalDeviceLimits(", // nested decode struct
		"func DecodeVkPhysicalDeviceProperties(",
		"v.DeviceType = dec.DecodeInt32() // enum VkPhysicalDeviceType",
		"v.MinMemoryMapAlignment = dec.DecodeSizeT()",                        // size_t member
		"v.DeviceName = string(dec.DecodeCharArray(int(n)))",                 // char[N]
		"v.PipelineCacheUUID = dec.DecodeUint8Array(int(n))",                 // uint8[N]
		"copy(v.MaxComputeWorkGroupCount[:], dec.DecodeUint32Array(int(n)))", // uint32[N]
		"copy(v.PointSizeRange[:], dec.DecodeFloat32Array(int(n)))",          // float[N]
		"v.MinTexelOffset = dec.DecodeInt32()",                               // int32 member
		"DecodeVkPhysicalDeviceLimits(dec, &v.Limits)",                       // nested-by-value decode
		"MinMemoryMapAlignment uint64",                                       // size_t input field (gofmt may re-pad)
	} {
		if !strings.Contains(s, want) {
			t.Errorf("generated decode source missing %q", want)
		}
	}
}

// TestEmitDecodeErrors covers the error/validation branches of the new decode
// and count-array emitters.
func TestEmitDecodeErrors(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{"VK_STRUCTURE_TYPE_X": "9"},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}

	// Unknown decode struct -> Generate error.
	if _, err := NewEmitter(reg, nil, nil).WithDecodeStructs([]string{"VkGhost"}).Generate("proof"); err == nil {
		t.Error("expected error for unknown decode struct")
	}

	// Unknown count-array reply command -> Generate error.
	if _, err := NewEmitter(reg, nil, nil).WithCountArrayReplies([]string{"vkGhost"}).Generate("proof"); err == nil {
		t.Error("expected error for unknown count-array reply command")
	}

	e := NewEmitter(reg, nil, nil)

	// Decode struct with an unsupported member type -> error (via decoder emit).
	reg.Structs["VkBadDec"] = &Struct{Name: "VkBadDec", Members: []*Member{{Name: "x", Type: "VkExotic"}}}
	var b bytes.Buffer
	if err := e.emitStructDecoder(&b, reg.Structs["VkBadDec"]); err == nil {
		t.Error("expected error from unsupported decode member type")
	}

	// Decode of a union -> error (returned unions out of scope).
	reg.Unions["VkBadUnionDec"] = true
	reg.Structs["VkBadUnionDec"] = &Struct{Name: "VkBadUnionDec", Members: []*Member{{Name: "a", Type: "float", FixedArrayLen: "4"}}}
	if err := e.emitStructDecoder(&b, reg.Structs["VkBadUnionDec"]); err == nil {
		t.Error("expected error decoding a union")
	}

	// Fixed-array decode error branches: multidim, unknown length, bad element.
	if err := e.emitFixedArrayMemberDecode(&b, "v.M", &Member{Name: "m", Type: "float", FixedArrayLen: "4[multidim]"}); err == nil {
		t.Error("expected multidim decode error")
	}
	if err := e.emitFixedArrayMemberDecode(&b, "v.M", &Member{Name: "m", Type: "uint8_t", FixedArrayLen: "VK_UNKNOWN"}); err == nil {
		t.Error("expected unknown-length decode error")
	}
	if err := e.emitFixedArrayMemberDecode(&b, "v.M", &Member{Name: "m", Type: "VkExotic", FixedArrayLen: "4"}); err == nil {
		t.Error("expected bad-element decode error")
	}

	// emitMemberDecode skips sType/pNext without writing.
	var sk bytes.Buffer
	if err := e.emitMemberDecode(&sk, "v", &Member{Name: "sType", IsSType: true}); err != nil || sk.Len() != 0 {
		t.Errorf("emitMemberDecode(sType) wrote %q err=%v", sk.String(), err)
	}
	if err := e.emitMemberDecode(&sk, "v", &Member{Name: "pNext", IsPNext: true}); err != nil || sk.Len() != 0 {
		t.Errorf("emitMemberDecode(pNext) wrote %q err=%v", sk.String(), err)
	}

	// Count-array reply with no VkCommandTypeEXT ordinal -> error.
	noOrd := &Command{Name: "vkNoOrd", Params: []*Param{
		{Name: "pCount", Type: "uint32_t", Pointer: true},
		{Name: "pArr", Type: "VkThing", Pointer: true, Len: "pCount"},
	}}
	reg.Handles["VkThing"] = true
	if err := e.emitCountArrayReplyDecoder(&b, noOrd); err == nil {
		t.Error("expected error for count-array reply without ordinal")
	}

	// Count-array reply whose params are not the count+array shape -> error.
	venusCommandTypeValues["vkNotShape"] = "995"
	defer delete(venusCommandTypeValues, "vkNotShape")
	notShape := &Command{Name: "vkNotShape", Params: []*Param{{Name: "device", Type: "VkDevice"}}}
	reg.Handles["VkDevice"] = true
	if err := e.emitCountArrayReplyDecoder(&b, notShape); err == nil {
		t.Error("expected error for non-count-array reply shape")
	}
}

// TestEmitDecodeStructSkipAndInt32Array covers the sType/pNext skip in a
// decode struct, the int32[N] fixed-array decode arm, and the already-visited
// branch of neededDecodeStructs (a nested struct shared by two parents).
func TestEmitDecodeStructSkipAndInt32Array(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	// A shared nested struct (visited once though referenced twice).
	reg.Structs["VkNested"] = &Struct{Name: "VkNested", Members: []*Member{
		{Name: "v", Type: "uint32_t"},
	}}
	// A decode struct carrying sType+pNext (skipped), an int32[2] fixed array,
	// and the shared nested struct.
	reg.Structs["VkDecHdr"] = &Struct{Name: "VkDecHdr", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "pNext", IsPNext: true},
		{Name: "offsets", Type: "int32_t", FixedArrayLen: "2"},
		{Name: "a", Type: "VkNested"},
		{Name: "b", Type: "VkNested"},
	}}
	reg.structOrder = []string{"VkNested", "VkDecHdr"}
	src, err := NewEmitter(reg, nil, nil).WithDecodeStructs([]string{"VkDecHdr"}).Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		"copy(v.Offsets[:], dec.DecodeInt32Array(int(n)))", // int32[N] arm
		"DecodeVkNested(dec, &v.A)",
		"DecodeVkNested(dec, &v.B)",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q\n%s", want, s)
		}
	}
	// sType/pNext must not produce a decode call.
	if strings.Contains(s, "v.SType") || strings.Contains(s, "v.PNext") {
		t.Error("sType/pNext should be skipped in the decoder")
	}
}

// TestGenerateDecodeErrorPropagation covers the error-return paths of the
// count-array and struct-decoder emitters as they propagate through Generate.
func TestGenerateDecodeErrorPropagation(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{"VkThing": true},
		Unions:     map[string]bool{},
	}
	// A decode struct with an unsupported member -> emitStructDecoder errors
	// inside Generate (the decodeNeeded loop).
	reg.Structs["VkBadDecGen"] = &Struct{Name: "VkBadDecGen", Members: []*Member{{Name: "x", Type: "VkExotic"}}}
	if _, err := NewEmitter(reg, nil, nil).WithDecodeStructs([]string{"VkBadDecGen"}).Generate("proof"); err == nil {
		t.Error("expected struct-decoder error through Generate")
	}

	// A count-array reply whose shape is wrong (a single by-value handle param)
	// errors in emitCountArrayReplyDecoder. The command's encoder succeeds
	// (ordinal present, handle param classifiable), so the error surfaces from
	// the count-array reply loop inside Generate, not the encoder loop.
	venusCommandTypeValues["vkBadShapeReply"] = "994"
	defer delete(venusCommandTypeValues, "vkBadShapeReply")
	reg.Commands["vkBadShapeReply"] = &Command{Name: "vkBadShapeReply", Params: []*Param{
		{Name: "thing", Type: "VkThing"},
	}}
	if _, err := NewEmitter(reg, nil, []string{"vkBadShapeReply"}).
		WithCountArrayReplies([]string{"vkBadShapeReply"}).Generate("proof"); err == nil {
		t.Error("expected count-array reply error through Generate")
	}
}

// TestEmitDecodeAllMemberScalars covers the remaining scalar decode branches
// (uint64, float, VkBool32, VkFlags, handle) via a synthetic decode struct.
func TestEmitDecodeAllMemberScalars(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{"VkThing": true},
		Unions:     map[string]bool{},
	}
	reg.Structs["VkDecScalars"] = &Struct{Name: "VkDecScalars", Members: []*Member{
		{Name: "a", Type: "uint64_t"},
		{Name: "b", Type: "float"},
		{Name: "c", Type: "VkBool32"},
		{Name: "d", Type: "VkFlags"},
		{Name: "h", Type: "VkThing"},
	}}
	reg.structOrder = []string{"VkDecScalars"}
	src, err := NewEmitter(reg, nil, nil).WithDecodeStructs([]string{"VkDecScalars"}).Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, want := range []string{
		"v.A = dec.DecodeUint64()",
		"v.B = dec.DecodeFloat32()",
		"v.C = dec.DecodeBool32()",
		"v.D = dec.DecodeFlags()",
		"v.H = dec.DecodeHandle() // handle VkThing",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("missing %q\n%s", want, src)
		}
	}
}

// TestEmitFixedArrayMembers covers the char[N]/uint8[N] fixed-array encode
// paths (not present in the curated proof structs) and the multidim/unknown
// error branches, via a synthetic registry.
func TestEmitFixedArrayMembers(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{"VK_STRUCTURE_TYPE_X": "9", "VK_UUID_SIZE": "16", "VK_MAX_PHYSICAL_DEVICE_NAME_SIZE": "256"},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	reg.Structs["VkProps"] = &Struct{Name: "VkProps", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "pNext", IsPNext: true},
		{Name: "deviceName", Type: "char", FixedArrayLen: "VK_MAX_PHYSICAL_DEVICE_NAME_SIZE"},
		{Name: "uuid", Type: "uint8_t", FixedArrayLen: "VK_UUID_SIZE"},
		{Name: "lits", Type: "uint32_t", FixedArrayLen: "3"},
	}}
	reg.structOrder = []string{"VkProps"}
	src, err := NewEmitter(reg, []string{"VkProps"}, nil).Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		"enc.EncodeArraySize(256)",      // char[256] array_size
		"buf := make([]byte, 256)",      // char blob staging
		"enc.EncodeBlobArray(buf)",      // char blob emit
		"enc.EncodeArraySize(16)",       // uint8[16] array_size
		"enc.EncodeUint8Array(v.Uuid)",  // uint8 array emit
		"enc.EncodeArraySize(3)",        // uint32[3] array_size (bare literal)
		"enc.EncodeUint32Array(v.Lits)", // uint32 array emit
		"DeviceName string",             // char[N] surfaced as string field
		"Uuid []byte",                   // uint8[N] surfaced as []byte field
		"Lits [3]uint32",                // uint32[N] surfaced as [3]uint32 field
	} {
		if !strings.Contains(s, want) {
			t.Errorf("fixed-array source missing %q\n%s", want, s)
		}
	}

	// Multi-dimensional fixed array -> error.
	reg.Structs["VkMulti"] = &Struct{Name: "VkMulti", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "m", Type: "float", FixedArrayLen: "4[multidim]"},
	}}
	if _, err := NewEmitter(reg, []string{"VkMulti"}, nil).Generate("proof"); err == nil {
		t.Error("expected error for multi-dimensional fixed array")
	}

	// Unknown fixed-array length -> error.
	reg.Structs["VkUnkLen"] = &Struct{Name: "VkUnkLen", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "m", Type: "uint8_t", FixedArrayLen: "VK_SOMETHING_UNKNOWN"},
	}}
	if _, err := NewEmitter(reg, []string{"VkUnkLen"}, nil).Generate("proof"); err == nil {
		t.Error("expected error for unknown fixed-array length")
	}

	// Unsupported fixed-array element type -> error.
	reg.Structs["VkBadElem"] = &Struct{Name: "VkBadElem", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "m", Type: "VkExotic", FixedArrayLen: "4"},
	}}
	if _, err := NewEmitter(reg, []string{"VkBadElem"}, nil).Generate("proof"); err == nil {
		t.Error("expected error for unsupported fixed-array element type")
	}
}

// TestEmitUnionError covers the union encoder's unsupported-arm branch.
func TestEmitUnionError(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{"VkBadUnion": true},
	}
	reg.Structs["VkBadUnion"] = &Struct{Name: "VkBadUnion", Members: []*Member{
		{Name: "weird", Type: "VkSomething", FixedArrayLen: "4"},
	}}
	reg.structOrder = []string{"VkBadUnion"}
	if _, err := NewEmitter(reg, []string{"VkBadUnion"}, nil).Generate("proof"); err == nil {
		t.Error("expected error for unsupported union arm type")
	}
}

// TestEmitParamAndSignatureErrors covers the unsupported command param branch
// in commandSignature/emitParam and the reply-without-handle error.
func TestEmitParamAndSignatureErrors(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	// A command whose param type is unsupported (not a handle/enum/scalar/struct).
	venusCommandTypeValues["vkBadParam"] = "999"
	defer delete(venusCommandTypeValues, "vkBadParam")
	reg.Commands["vkBadParam"] = &Command{Name: "vkBadParam", Params: []*Param{
		{Name: "x", Type: "VkExoticByValue"},
	}}
	if _, err := NewEmitter(reg, nil, []string{"vkBadParam"}).Generate("proof"); err == nil {
		t.Error("expected error for unsupported command param")
	}

	// A reply for a command with no handle out-param -> reply-decode error.
	venusCommandTypeValues["vkNoHandleReply"] = "998"
	defer delete(venusCommandTypeValues, "vkNoHandleReply")
	reg.Commands["vkNoHandleReply"] = &Command{Name: "vkNoHandleReply", Params: []*Param{
		{Name: "device", Type: "VkDevice"},
	}}
	reg.Handles["VkDevice"] = true
	// device is a by-value handle (not pointer), so there is no handle out-param.
	if _, err := NewEmitter(reg, nil, []string{"vkNoHandleReply"}).WithReplies([]string{"vkNoHandleReply"}).Generate("proof"); err == nil {
		t.Error("expected error for reply decode without a handle out-param")
	}

	// Reply for an unknown command -> error.
	if _, err := NewEmitter(reg, nil, nil).WithReplies([]string{"vkGhost"}).Generate("proof"); err == nil {
		t.Error("expected error for unknown reply command")
	}
}

// TestScalarArrayPrimAndFixedArrayN unit-covers the small helpers.
func TestScalarArrayPrimAndFixedArrayN(t *testing.T) {
	if scalarArrayPrim("float") != "EncodeFloat32Array" ||
		scalarArrayPrim("uint32_t") != "EncodeUint32Array" ||
		scalarArrayPrim("int32_t") != "EncodeInt32Array" ||
		scalarArrayPrim("char") != "" {
		t.Error("scalarArrayPrim")
	}
	e := &Emitter{reg: &Registry{EnumValues: map[string]string{"VK_UUID_SIZE": "16"}}}
	if e.fixedArrayN("VK_UUID_SIZE") != "16" || e.fixedArrayN("7") != "7" ||
		e.fixedArrayN("") != "" || e.fixedArrayN("Nope") != "" {
		t.Error("fixedArrayN")
	}
}

// TestEmitMemberExtraShapes covers the VkBool32 member, the int32 counted
// scalar array member, and the float fixed-array member.
func TestEmitMemberExtraShapes(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{"VK_STRUCTURE_TYPE_X": "9"},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	reg.Structs["VkShapes"] = &Struct{Name: "VkShapes", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "pNext", IsPNext: true},
		{Name: "enabled", Type: "VkBool32"},
		{Name: "samples", Type: "int32_t", Pointer: true, Len: "sampleCount"},
		{Name: "coeffs", Type: "float", FixedArrayLen: "4"},
	}}
	reg.structOrder = []string{"VkShapes"}
	src, err := NewEmitter(reg, []string{"VkShapes"}, nil).Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, want := range []string{
		"enc.EncodeBool32(v.Enabled)",
		"enc.EncodeInt32Array(v.Samples)",  // counted int32 scalar array
		"enc.EncodeFloat32Array(v.Coeffs)", // float fixed array
		"enc.EncodeArraySize(4)",           // float fixed-array prefix
		"Enabled bool",
		"Samples []int32",
		"Coeffs [4]float32",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("missing %q\n%s", want, src)
		}
	}
}

// TestEmitCommandFlagAndCountParams covers the by-value VkFlags param and the
// uint32 out-count pointer param in a command signature/body.
func TestEmitCommandFlagAndCountParams(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{"VkDevice": true},
		Unions:     map[string]bool{},
	}
	venusCommandTypeValues["vkFancy"] = "900"
	defer delete(venusCommandTypeValues, "vkFancy")
	reg.Commands["vkFancy"] = &Command{Name: "vkFancy", Params: []*Param{
		{Name: "device", Type: "VkDevice"},
		{Name: "mask", Type: "VkSampleCountFlags"},
		{Name: "pCount", Type: "uint32_t", Pointer: true},
	}}
	src, err := NewEmitter(reg, nil, []string{"vkFancy"}).Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, want := range []string{
		"mask uint32",
		"enc.EncodeFlags(mask)",
		"pCount uint32",
		"enc.EncodeUint32(pCount)",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("missing %q\n%s", want, src)
		}
	}
}

// TestEmitReplyUnknownCmdType covers the no-VkCommandTypeEXT-value branch of
// the reply decoder emitter.
func TestEmitReplyUnknownCmdType(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{"VkThing": true},
		Unions:     map[string]bool{},
	}
	cmd := &Command{Name: "vkNoOrdinal", Params: []*Param{
		{Name: "pThing", Type: "VkThing", Pointer: true},
	}}
	reg.Commands["vkNoOrdinal"] = cmd
	// vkNoOrdinal has no venusCommandTypeValues entry; the reply decoder must
	// surface that. Call emitCommandReplyDecoder directly to isolate this
	// branch (via Generate the encoder would error first on the same lookup).
	var b bytes.Buffer
	if err := NewEmitter(reg, nil, nil).emitCommandReplyDecoder(&b, cmd); err == nil {
		t.Error("expected error for command without VkCommandTypeEXT ordinal")
	}
}

// TestEmitDefensiveBranches covers the error-propagation and skip branches
// that Generate's ordering otherwise shadows, by calling the emit helpers
// directly with crafted inputs.
func TestEmitDefensiveBranches(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{"VK_STRUCTURE_TYPE_X": "9"},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	e := NewEmitter(reg, nil, nil)

	// emitMember on sType/pNext members returns nil without writing (the skip
	// guard) — exercised directly since real structs filter these earlier.
	var b bytes.Buffer
	if err := e.emitMember(&b, "v", &Member{Name: "sType", IsSType: true}); err != nil || b.Len() != 0 {
		t.Errorf("emitMember(sType) wrote %q err=%v", b.String(), err)
	}
	if err := e.emitMember(&b, "v", &Member{Name: "pNext", IsPNext: true}); err != nil || b.Len() != 0 {
		t.Errorf("emitMember(pNext) wrote %q err=%v", b.String(), err)
	}

	// emitMember error propagation through a PLAIN (sType-less) struct encoder.
	plainBad := &Struct{Name: "VkPlainBad", Members: []*Member{{Name: "x", Type: "VkExotic"}}}
	var pb bytes.Buffer
	if err := e.emitStructEncoder(&pb, plainBad); err == nil {
		t.Error("expected error from plain-struct member emit")
	}

	// An unsupported command param is rejected by commandSignature (and so by
	// emitCommandEncoder) before emitParam is reached.
	venusCommandTypeValues["vkBadBody"] = "997"
	defer delete(venusCommandTypeValues, "vkBadBody")
	badBody := &Command{Name: "vkBadBody", Params: []*Param{{Name: "x", Type: "VkExoticByValue"}}}
	var cb bytes.Buffer
	if err := e.emitCommandEncoder(&cb, badBody); err == nil {
		t.Error("expected error from emitCommandEncoder body")
	}
	// emitParam's default leaf is an unreachable invariant guard: calling it on
	// an unclassified param (bypassing commandSignature) panics.
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected emitParam to panic on an unclassified param")
			}
		}()
		e.emitParam(&cb, &Param{Name: "x", Type: "VkExoticByValue"})
	}()

	// commandSignature with only a (NULL) VkAllocationCallbacks param -> empty
	// signature ("" return).
	venusCommandTypeValues["vkNoArgs"] = "996"
	defer delete(venusCommandTypeValues, "vkNoArgs")
	noArgs := &Command{Name: "vkNoArgs", Params: []*Param{{Name: "pAllocator", Type: "VkAllocationCallbacks", Pointer: true}}}
	sig, err := e.commandSignature(noArgs)
	if err != nil || sig != "" {
		t.Errorf("commandSignature(no encodable args) = %q err=%v", sig, err)
	}
}

// TestParseFixedArrayLenNoClose covers the unbalanced-bracket branch
// (a '[' with no ']').
func TestParseFixedArrayLenNoClose(t *testing.T) {
	if got := parseFixedArrayLen(`<type>char</type> <name>x</name>[oops`); got != "" {
		t.Errorf("parseFixedArrayLen(no close) = %q want \"\"", got)
	}
}

// TestParseFixedArrayLen covers the array-dimension recovery branches.
func TestParseFixedArrayLen(t *testing.T) {
	cases := map[string]string{
		`<type>uint8_t</type> <name>uuid</name>[<enum>VK_UUID_SIZE</enum>]`: "VK_UUID_SIZE",
		`<type>float</type> <name>m</name>[4]`:                              "4",
		`<type>uint32_t</type> <name>x</name>`:                              "",
		`<type>float</type> <name>matrix</name>[3][4]`:                      "3[multidim]",
		`no name here`:                       "",
		`<name>x</name> trailing-no-bracket`: "",
	}
	for raw, want := range cases {
		if got := parseFixedArrayLen(raw); got != want {
			t.Errorf("parseFixedArrayLen(%q) = %q want %q", raw, got, want)
		}
	}
}
