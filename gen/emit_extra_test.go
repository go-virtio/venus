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
