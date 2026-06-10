package proof

import (
	"bytes"
	"testing"

	"github.com/go-virtio/venus/internal/vncs"
)

// le32 / le64 build little-endian dwords/qwords for the hand-derived
// expected byte streams below.
func le32(v uint32) []byte { return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)} }
func le64(v uint64) []byte {
	return []byte{
		byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24),
		byte(v >> 32), byte(v >> 40), byte(v >> 48), byte(v >> 56),
	}
}

// str builds the Venus on-wire encoding of a Go string s:
//
//	array_size(len(s)+1)  [8-byte LE]   (Mesa vn_encode_array_size)
//	bytes(s) + NUL, zero-padded to a multiple of 4 (Mesa vn_encode_char_array)
func str(s string) []byte {
	out := le64(uint64(len(s) + 1))
	raw := append([]byte(s), 0)
	for len(raw)%4 != 0 {
		raw = append(raw, 0)
	}
	return append(out, raw...)
}

// TestPrimitives validates the named primitive proof encoders against
// hand-derived Mesa bytes (the runtime is exercised more exhaustively in
// internal/vncs; this confirms the proof-package wrappers wire through).
func TestPrimitives(t *testing.T) {
	cases := []struct {
		name string
		run  func(*vncs.Encoder)
		want []byte
	}{
		{"uint32", func(e *vncs.Encoder) { EncodeUint32(e, 0x11223344) }, le32(0x11223344)},
		{"int32", func(e *vncs.Encoder) { EncodeInt32(e, -2) }, le32(0xfffffffe)},
		{"uint64", func(e *vncs.Encoder) { EncodeUint64(e, 0x1122334455667788) }, le64(0x1122334455667788)},
		{"float32", func(e *vncs.Encoder) { EncodeFloat32(e, 2.0) }, le32(0x40000000)}, // 2.0f
		{"flags", func(e *vncs.Encoder) { EncodeFlags(e, 0xDEADBEEF) }, le32(0xDEADBEEF)},
		{"handle", func(e *vncs.Encoder) { EncodeHandle(e, 0x42) }, le64(0x42)},
		{"array_size", func(e *vncs.Encoder) { EncodeArraySize(e, 7) }, le64(7)},
		{"string", func(e *vncs.Encoder) { EncodeString(e, "abc") }, str("abc")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := vncs.NewEncoder()
			tc.run(e)
			if !bytes.Equal(e.Bytes(), tc.want) {
				t.Fatalf("%s: got % x want % x", tc.name, e.Bytes(), tc.want)
			}
		})
	}
}

// TestEncodeVkApplicationInfo derives the full byte stream from Mesa
// vn_encode_VkApplicationInfo / _self (vn_protocol_driver_instance.h):
//
//	vn_encode_VkStructureType(&VK_STRUCTURE_TYPE_APPLICATION_INFO)  -> int32 0
//	vn_encode_VkApplicationInfo_pnext(NULL) -> simple_pointer(NULL) -> array_size(0)
//	// self:
//	if (pApplicationName) { array_size(strlen+1); char_array }    // "demo"
//	vn_encode_uint32_t(&applicationVersion)
//	if (pEngineName) { array_size(strlen+1); char_array }         // "eng"
//	vn_encode_uint32_t(&engineVersion)
//	vn_encode_uint32_t(&apiVersion)
func TestEncodeVkApplicationInfo(t *testing.T) {
	app := &VkApplicationInfo{
		PApplicationName:   "demo",
		ApplicationVersion: 0x010203,
		PEngineName:        "eng",
		EngineVersion:      0x040506,
		ApiVersion:         0x00401000, // VK_API_VERSION_1_1 style value
	}
	e := vncs.NewEncoder()
	EncodeVkApplicationInfo(e, app)

	var want []byte
	want = append(want, le32(0)...)          // sType = VK_STRUCTURE_TYPE_APPLICATION_INFO (0)
	want = append(want, le64(0)...)          // pNext simple_pointer(NULL)
	want = append(want, str("demo")...)      // pApplicationName
	want = append(want, le32(0x010203)...)   // applicationVersion
	want = append(want, str("eng")...)       // pEngineName
	want = append(want, le32(0x040506)...)   // engineVersion
	want = append(want, le32(0x00401000)...) // apiVersion

	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkApplicationInfo\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkApplicationInfoEmptyStrings exercises the NULL-string branch
// (Mesa: else { vn_encode_array_size(enc, 0); }).
func TestEncodeVkApplicationInfoEmptyStrings(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkApplicationInfo(e, &VkApplicationInfo{ApiVersion: 1})
	var want []byte
	want = append(want, le32(0)...) // sType
	want = append(want, le64(0)...) // pNext NULL
	want = append(want, le64(0)...) // pApplicationName NULL -> array_size(0)
	want = append(want, le32(0)...) // applicationVersion
	want = append(want, le64(0)...) // pEngineName NULL -> array_size(0)
	want = append(want, le32(0)...) // engineVersion
	want = append(want, le32(1)...) // apiVersion
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("empty-string ApplicationInfo\n got % x\nwant % x", e.Bytes(), want)
	}
}

// sampleICI is the concrete VkInstanceCreateInfo used for the
// byte-for-byte validation and the command-framing validation.
func sampleICI() *VkInstanceCreateInfo {
	return &VkInstanceCreateInfo{
		Flags: 0,
		PApplicationInfo: &VkApplicationInfo{
			PApplicationName:   "venus",
			ApplicationVersion: 1,
			PEngineName:        "", // NULL engine name
			EngineVersion:      0,
			ApiVersion:         0x00402000,
		},
		EnabledLayerCount:       1,
		PpEnabledLayerNames:     []string{"VK_LAYER_KHRONOS_validation"},
		EnabledExtensionCount:   2,
		PpEnabledExtensionNames: []string{"VK_KHR_surface", "VK_EXT_debug_utils"},
	}
}

// iciExpected hand-derives the byte stream for sampleICI from Mesa
// vn_encode_VkInstanceCreateInfo / _self (vn_protocol_driver_instance.h).
func iciExpected() []byte {
	var w []byte
	// vn_encode_VkInstanceCreateInfo:
	w = append(w, le32(1)...) // sType = VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO (1)
	w = append(w, le64(0)...) // _pnext: simple_pointer(NULL)
	// _self:
	w = append(w, le32(0)...) // flags (VkFlags = 0)
	// pApplicationInfo: simple_pointer(present)=1 then nested VkApplicationInfo
	w = append(w, le64(1)...)
	{ // nested vn_encode_VkApplicationInfo
		w = append(w, le32(0)...)      // sType = APPLICATION_INFO (0)
		w = append(w, le64(0)...)      // pNext NULL
		w = append(w, str("venus")...) // pApplicationName
		w = append(w, le32(1)...)      // applicationVersion
		w = append(w, le64(0)...)      // pEngineName NULL -> array_size(0)
		w = append(w, le32(0)...)      // engineVersion
		w = append(w, le32(0x00402000)...)
	}
	w = append(w, le32(1)...) // enabledLayerCount
	// ppEnabledLayerNames present: array_size(count) then each string
	w = append(w, le64(1)...)
	w = append(w, str("VK_LAYER_KHRONOS_validation")...)
	w = append(w, le32(2)...) // enabledExtensionCount
	// ppEnabledExtensionNames present: array_size(count) then strings
	w = append(w, le64(2)...)
	w = append(w, str("VK_KHR_surface")...)
	w = append(w, str("VK_EXT_debug_utils")...)
	return w
}

func TestEncodeVkInstanceCreateInfo(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkInstanceCreateInfo(e, sampleICI())
	want := iciExpected()
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("VkInstanceCreateInfo\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkInstanceCreateInfoNilArrays exercises the NULL-pointer /
// empty-array branches: pApplicationInfo NULL, no layers, no extensions.
func TestEncodeVkInstanceCreateInfoNilArrays(t *testing.T) {
	e := vncs.NewEncoder()
	EncodeVkInstanceCreateInfo(e, &VkInstanceCreateInfo{})
	var w []byte
	w = append(w, le32(1)...) // sType
	w = append(w, le64(0)...) // pNext NULL
	w = append(w, le32(0)...) // flags
	w = append(w, le64(0)...) // pApplicationInfo simple_pointer(NULL)
	w = append(w, le32(0)...) // enabledLayerCount
	w = append(w, le64(0)...) // ppEnabledLayerNames NULL -> array_size(0)
	w = append(w, le32(0)...) // enabledExtensionCount
	w = append(w, le64(0)...) // ppEnabledExtensionNames NULL -> array_size(0)
	if !bytes.Equal(e.Bytes(), w) {
		t.Fatalf("nil-array ICI\n got % x\nwant % x", e.Bytes(), w)
	}
}

// TestEncodeVkCreateInstance derives the full command buffer from Mesa
// vn_encode_vkCreateInstance (vn_protocol_driver_instance.h):
//
//	vn_encode_VkCommandTypeEXT(&VK_COMMAND_TYPE_vkCreateInstance_EXT) -> int32 0
//	vn_encode_VkFlags(&cmd_flags)                                     -> uint32
//	if (simple_pointer(pCreateInfo)) vn_encode_VkInstanceCreateInfo(...)
//	if (simple_pointer(pAllocator))  assert(false)   // always NULL
//	if (simple_pointer(pInstance))   vn_encode_VkInstance(pInstance)
func TestEncodeVkCreateInstance(t *testing.T) {
	ici := sampleICI()
	const pInstance = uint64(0) // NULL out-handle: vn_cs_handle_load_id(NULL)=0

	e := vncs.NewEncoder()
	Encode_vkCreateInstance(e, 0, ici, pInstance)

	var want []byte
	want = append(want, le32(0)...) // cmd_type = VK_COMMAND_TYPE_vkCreateInstance_EXT (0)
	want = append(want, le32(0)...) // cmd_flags = 0
	want = append(want, le64(1)...) // simple_pointer(pCreateInfo) = present
	want = append(want, iciExpected()...)
	want = append(want, le64(0)...) // simple_pointer(pAllocator) = NULL
	want = append(want, le64(0)...) // simple_pointer(pInstance): id 0 -> absent

	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("vkCreateInstance\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkCreateInstancePresentHandle exercises the present-handle
// branch (pInstance id != 0 -> simple_pointer(present) + handle id).
func TestEncodeVkCreateInstancePresentHandle(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkCreateInstance(e, 0x7, &VkInstanceCreateInfo{}, 0x99)
	var want []byte
	want = append(want, le32(0)...) // cmd_type
	want = append(want, le32(0x7)...)
	want = append(want, le64(1)...) // pCreateInfo present
	// minimal ICI (nil arrays)
	want = append(want, le32(1)...) // sType
	want = append(want, le64(0)...) // pNext NULL
	want = append(want, le32(0)...) // flags
	want = append(want, le64(0)...) // pApplicationInfo NULL
	want = append(want, le32(0)...) // enabledLayerCount
	want = append(want, le64(0)...) // ppEnabledLayerNames array_size(0)
	want = append(want, le32(0)...) // enabledExtensionCount
	want = append(want, le64(0)...) // ppEnabledExtensionNames array_size(0)
	want = append(want, le64(0)...) // pAllocator NULL
	want = append(want, le64(1)...) // pInstance present
	want = append(want, le64(0x99)...)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("present-handle\n got % x\nwant % x", e.Bytes(), want)
	}
}

// TestEncodeVkCreateInstanceNilCreateInfo exercises the absent-pCreateInfo
// branch (simple_pointer(NULL), no nested struct).
func TestEncodeVkCreateInstanceNilCreateInfo(t *testing.T) {
	e := vncs.NewEncoder()
	Encode_vkCreateInstance(e, 0, nil, 0)
	want := bytes.Join([][]byte{
		le32(0), // cmd_type
		le32(0), // cmd_flags
		le64(0), // pCreateInfo NULL
		le64(0), // pAllocator NULL
		le64(0), // pInstance absent
	}, nil)
	if !bytes.Equal(e.Bytes(), want) {
		t.Fatalf("nil-createinfo\n got % x\nwant % x", e.Bytes(), want)
	}
}
