package gen

import (
	"os"
	"strings"
	"testing"
)

func loadSubset(t *testing.T) *Registry {
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

func TestParseModel(t *testing.T) {
	reg := loadSubset(t)

	ici := reg.Structs["VkInstanceCreateInfo"]
	if ici == nil {
		t.Fatal("VkInstanceCreateInfo not parsed")
	}
	// Member order and classification.
	byName := map[string]*Member{}
	for _, m := range ici.Members {
		byName[m.Name] = m
	}
	if !byName["sType"].IsSType || byName["sType"].STypeValue != "VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO" {
		t.Errorf("sType not classified: %+v", byName["sType"])
	}
	if !byName["pNext"].IsPNext {
		t.Error("pNext not classified")
	}
	if !byName["pApplicationInfo"].Pointer || byName["pApplicationInfo"].Type != "VkApplicationInfo" {
		t.Errorf("pApplicationInfo: %+v", byName["pApplicationInfo"])
	}
	if !byName["ppEnabledLayerNames"].PointerToConst {
		t.Errorf("ppEnabledLayerNames should be PointerToConst: %+v", byName["ppEnabledLayerNames"])
	}
	if byName["ppEnabledLayerNames"].Len != "enabledLayerCount,null-terminated" {
		t.Errorf("len attr lost: %q", byName["ppEnabledLayerNames"].Len)
	}
	if !byName["flags"].Optional {
		t.Error("flags should be optional")
	}

	app := reg.Structs["VkApplicationInfo"]
	pName := map[string]*Member{}
	for _, m := range app.Members {
		pName[m.Name] = m
	}
	if !pName["pApplicationName"].Pointer || pName["pApplicationName"].Type != "char" {
		t.Errorf("pApplicationName: %+v", pName["pApplicationName"])
	}

	cmd := reg.Commands["vkCreateInstance"]
	if cmd == nil || len(cmd.Params) != 3 {
		t.Fatalf("vkCreateInstance params: %+v", cmd)
	}
	if !cmd.Params[0].Pointer || cmd.Params[0].Type != "VkInstanceCreateInfo" {
		t.Errorf("param0: %+v", cmd.Params[0])
	}
	if !cmd.Params[1].Optional || cmd.Params[1].Type != "VkAllocationCallbacks" {
		t.Errorf("param1: %+v", cmd.Params[1])
	}

	if reg.EnumValues["VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO"] != "1" {
		t.Errorf("enum value: %q", reg.EnumValues["VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO"])
	}
}

func TestStructNamesDeterministic(t *testing.T) {
	reg := loadSubset(t)
	names := reg.StructNames()
	// Document order: nested-by-value structs come first in the testdata.
	if len(names) == 0 || names[0] != "VkExtent3D" {
		t.Errorf("StructNames order: %v", names)
	}
	if sorted := SortedStructNames(reg); sorted[0] != "VkApplicationInfo" || sorted[1] != "VkBufferMemoryBarrier" {
		t.Errorf("SortedStructNames: %v", sorted)
	}
}

func TestEmitGeneratesExpectedFunctions(t *testing.T) {
	reg := loadSubset(t)
	src, err := NewEmitter(reg, []string{"VkInstanceCreateInfo"}, []string{"vkCreateInstance"}).Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	s := string(src)
	for _, want := range []string{
		"func EncodeVkApplicationInfo(enc *vncs.Encoder, v *VkApplicationInfo)",
		"func EncodeVkInstanceCreateInfo(enc *vncs.Encoder, v *VkInstanceCreateInfo)",
		"func Encode_vkCreateInstance(enc *vncs.Encoder, cmdFlags uint32, pCreateInfo *VkInstanceCreateInfo, pInstance uint64)",
		"enc.EncodeInt32(0) // sType = VK_STRUCTURE_TYPE_APPLICATION_INFO",
		"enc.EncodeInt32(1) // sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO",
		"PpEnabledLayerNames",
		"[]string",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("generated source missing %q\n---\n%s", want, s)
		}
	}
	// VkApplicationInfo must be emitted before VkInstanceCreateInfo (nested
	// before referencing).
	if strings.Index(s, "func EncodeVkApplicationInfo") > strings.Index(s, "func EncodeVkInstanceCreateInfo") {
		t.Error("nested struct not emitted before referencing struct")
	}
}

func TestEmitErrors(t *testing.T) {
	reg := loadSubset(t)
	if _, err := NewEmitter(reg, []string{"Nope"}, nil).Generate("proof"); err == nil {
		t.Error("expected error for unknown struct")
	}
	if _, err := NewEmitter(reg, nil, []string{"vkNope"}).Generate("proof"); err == nil {
		t.Error("expected error for unknown command")
	}
}

// TestParseSkipsNonStructAndUnnamed covers the parse-side skip branches:
// a non-struct <type>, an unnamed <type>, and a command with no <proto>
// name (all of which appear in the real vk.xml and must be ignored).
func TestParseSkipsNonStructAndUnnamed(t *testing.T) {
	xml := `<registry>
	  <types>
	    <type category="basetype">typedef uint32_t <name>VkFlags</name>;</type>
	    <type category="struct"><member><type>int</type> <name>x</name></member></type>
	    <type category="struct" name="VkOk"><member values="VK_STRUCTURE_TYPE_OK"><type>VkStructureType</type> <name>sType</name></member></type>
	  </types>
	  <enums><enum value="3" name="VK_STRUCTURE_TYPE_OK"/></enums>
	  <commands>
	    <command><proto><type>void</type></proto></command>
	    <command><proto><type>void</type> <name>vkOk</name></proto></command>
	  </commands>
	</registry>`
	reg, err := Parse([]byte(xml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(reg.StructNames()) != 1 || reg.Structs["VkOk"] == nil {
		t.Errorf("non-struct/unnamed type not skipped: %v", reg.StructNames())
	}
	if reg.Commands["vkOk"] == nil || len(reg.Commands) != 1 {
		t.Errorf("unnamed command not skipped: %v", reg.Commands)
	}
}

func TestParseError(t *testing.T) {
	if _, err := Parse([]byte("<registry><types><type")); err == nil {
		t.Error("expected parse error on malformed xml")
	}
}

func TestHelpers(t *testing.T) {
	if leadingOptional("") || !leadingOptional("true") || leadingOptional("false,true") {
		t.Error("leadingOptional")
	}
	if exportName("") != "" || exportName("flags") != "Flags" {
		t.Error("exportName")
	}
	if lowerName("") != "" || lowerName("PCreateInfo") != "pCreateInfo" {
		t.Error("lowerName")
	}
	if rawBetween("no-type-here") != "" {
		t.Error("rawBetween without </type>")
	}
	// rawBetween with </type> but no <name> returns the tail.
	if got := rawBetween("</type>* tail"); got != "* tail" {
		t.Errorf("rawBetween tail: %q", got)
	}
	if collapseSpaces("a   b\tc") != "a b c" {
		t.Error("collapseSpaces")
	}
}

// TestGoTypeOf covers the type-mapping branches not all reachable from the
// proof subset's emit path (e.g. int32/uint64/float/embedded struct, enums,
// handles, fixed arrays, counted-pointer slices).
func TestGoTypeOf(t *testing.T) {
	reg := &Registry{
		Structs:   map[string]*Struct{"VkApplicationInfo": {}, "VkExtent3D": {}, "VkDeviceQueueCreateInfo": {}},
		EnumTypes: map[string]bool{"VkImageLayout": true},
		Handles:   map[string]bool{"VkImage": true},
		EnumValues: map[string]string{
			"VK_UUID_SIZE": "16",
		},
	}
	cases := []struct {
		m    *Member
		want string
	}{
		{&Member{Type: "char", Pointer: true}, "string"},
		{&Member{PointerToConst: true}, "[]string"},
		{&Member{IsPNext: true}, ""},
		{&Member{Type: "VkStructureType"}, ""},
		{&Member{Type: "uint32_t"}, "uint32"},
		{&Member{Type: "int32_t"}, "int32"},
		{&Member{Type: "uint64_t"}, "uint64"},
		{&Member{Type: "float"}, "float32"},
		{&Member{Type: "VkFlags"}, "uint32"},
		{&Member{Type: "VkInstanceCreateFlags"}, "uint32"},
		{&Member{Type: "VkImageUsageFlags"}, "uint32"},     // Vk*Flags alias
		{&Member{Type: "VkSampleCountFlagBits"}, "uint32"}, // Vk*FlagBits alias
		{&Member{Type: "VkBool32"}, "bool"},
		{&Member{Type: "VkDeviceSize"}, "uint64"},
		{&Member{Type: "VkDeviceAddress"}, "uint64"},
		{&Member{Type: "VkImageLayout"}, "int32"}, // enum
		{&Member{Type: "VkImage"}, "uint64"},      // handle
		{&Member{Type: "VkApplicationInfo", Pointer: true}, "*VkApplicationInfo"},
		{&Member{Type: "VkExtent3D"}, "VkExtent3D"}, // nested-by-value
		// counted-pointer slices
		{&Member{Type: "float", Pointer: true, Len: "queueCount"}, "[]float32"},
		{&Member{Type: "uint32_t", Pointer: true, Len: "n"}, "[]uint32"},
		{&Member{Type: "int32_t", Pointer: true, Len: "n"}, "[]int32"},
		{&Member{Type: "VkDeviceQueueCreateInfo", Pointer: true, Len: "n"}, "[]VkDeviceQueueCreateInfo"},
		// fixed arrays
		{&Member{Type: "char", FixedArrayLen: "VK_UUID_SIZE"}, "string"},
		{&Member{Type: "uint8_t", FixedArrayLen: "VK_UUID_SIZE"}, "[]byte"},
		{&Member{Type: "uint32_t", FixedArrayLen: "4"}, "[4]uint32"},
		{&Member{Type: "int32_t", FixedArrayLen: "4"}, "[4]int32"},
		{&Member{Type: "float", FixedArrayLen: "4"}, "[4]float32"},
		{&Member{Type: "uint32_t", FixedArrayLen: "VK_UUID_SIZE"}, "[16]uint32"},
	}
	for _, c := range cases {
		if got := goTypeOf(reg, c.m); got != c.want {
			t.Errorf("goTypeOf(%+v) = %q want %q", c.m, got, c.want)
		}
	}
}

// TestEmitScalarMembers covers the scalar member-encode branches
// (int32/uint64/float/VkFlags) and the unsupported-type error, which the
// proof subset alone does not reach.
func TestEmitScalarMembers(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{"VK_STRUCTURE_TYPE_X": "9"},
	}
	reg.Structs["VkX"] = &Struct{Name: "VkX", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "pNext", IsPNext: true},
		{Name: "a", Type: "int32_t"},
		{Name: "b", Type: "uint64_t"},
		{Name: "c", Type: "float"},
		{Name: "d", Type: "VkFlags"},
	}}
	reg.structOrder = []string{"VkX"}
	src, err := NewEmitter(reg, []string{"VkX"}, nil).Generate("proof")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for _, want := range []string{"EncodeInt32(v.A)", "EncodeUint64(v.B)", "EncodeFloat32(v.C)", "EncodeFlags(v.D)"} {
		if !strings.Contains(string(src), want) {
			t.Errorf("missing %q in:\n%s", want, src)
		}
	}

	// Unsupported member type -> error.
	reg.Structs["VkBad"] = &Struct{Name: "VkBad", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_X"},
		{Name: "weird", Type: "VkSomethingExotic"},
	}}
	if _, err := NewEmitter(reg, []string{"VkBad"}, nil).Generate("proof"); err == nil {
		t.Error("expected error for unsupported member type")
	}

	// Struct without sType -> emitted as a plain nested-by-value struct
	// (no sType/pNext prologue), e.g. VkExtent3D / VkImageSubresourceRange.
	reg.Structs["VkNoSType"] = &Struct{Name: "VkNoSType", Members: []*Member{{Name: "x", Type: "uint32_t"}}}
	noType, err := NewEmitter(reg, []string{"VkNoSType"}, nil).Generate("proof")
	if err != nil {
		t.Fatalf("sType-less struct should emit, got error: %v", err)
	}
	if strings.Contains(string(noType), "pNext = NULL") {
		t.Error("plain struct must not emit an sType/pNext prologue")
	}
	if !strings.Contains(string(noType), "EncodeUint32(v.X)") {
		t.Errorf("plain struct missing member encode:\n%s", noType)
	}

	// sType token without a known enum value -> error.
	reg.Structs["VkBadEnum"] = &Struct{Name: "VkBadEnum", Members: []*Member{
		{Name: "sType", IsSType: true, STypeValue: "VK_STRUCTURE_TYPE_UNKNOWN"},
	}}
	if _, err := NewEmitter(reg, []string{"VkBadEnum"}, nil).Generate("proof"); err == nil {
		t.Error("expected error for unknown sType enum value")
	}
}

// TestEmitCommandUnknownType covers the no-VkCommandTypeEXT-value error.
func TestEmitCommandUnknownType(t *testing.T) {
	reg := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
	}
	reg.Commands["vkUnknown"] = &Command{Name: "vkUnknown", Params: []*Param{
		{Name: "x", Type: "uint32_t"},
	}}
	if _, err := NewEmitter(reg, nil, []string{"vkUnknown"}).Generate("proof"); err == nil {
		t.Error("expected error for unknown command type value")
	}
}
