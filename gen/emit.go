package gen

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
)

// Emitter renders Go encoder functions for a chosen set of structs and
// commands from a parsed Registry. The emitted code targets package proof
// and calls the vncs runtime.
//
// The walk mirrors Mesa's generator: for each struct it emits an
// Encode<Struct> that writes sType (int32), then the pNext chain
// (simple_pointer NULL for M0), then each "self" member in declaration
// order; for each command it emits an Encode_<cmd> that writes the
// VkCommandTypeEXT (int32), the command-flags dword (VkFlags), then each
// param as an optional simple_pointer + pointee.
type Emitter struct {
	reg *Registry

	// requested structs/commands, in emission order
	structs  []string
	commands []string
}

// NewEmitter builds an Emitter over reg for the named structs and commands.
func NewEmitter(reg *Registry, structs, commands []string) *Emitter {
	return &Emitter{reg: reg, structs: structs, commands: commands}
}

// goTypeOf maps a Vulkan member type onto the Go parameter type used by the
// generated encoder's input struct fields.
func goTypeOf(m *Member) string {
	switch {
	case m.PointerToConst: // const char* const*
		return "[]string"
	case m.Type == "char" && m.Pointer:
		return "string"
	case m.IsPNext:
		return "" // pNext is not a generated field for M0
	}
	switch m.Type {
	case "uint32_t":
		return "uint32"
	case "int32_t":
		return "int32"
	case "uint64_t":
		return "uint64"
	case "float":
		return "float32"
	case "VkFlags", "VkInstanceCreateFlags":
		return "uint32"
	case "VkStructureType":
		return "" // synthesised from the known sType value
	}
	if m.Pointer {
		// Pointer to a nested struct, e.g. pApplicationInfo. Represented as
		// a Go pointer to the nested input struct.
		return "*" + m.Type
	}
	// A nested struct embedded by value (none in the M0 proof set).
	return m.Type
}

// Generate renders the full proof package file (header + input structs +
// encoders) as formatted-ready Go source (gofmt is applied by the caller).
func (e *Emitter) Generate(pkg string) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString(fileHeader(pkg))

	// Every explicitly-requested struct/command must exist.
	for _, name := range e.structs {
		if e.reg.Structs[name] == nil {
			return nil, fmt.Errorf("gen: struct %q not found in registry", name)
		}
	}
	for _, name := range e.commands {
		if e.reg.Commands[name] == nil {
			return nil, fmt.Errorf("gen: command %q not found in registry", name)
		}
	}

	// Collect the nested structs the requested structs/commands reference so
	// we can emit input types for them too (deterministic, deduplicated).
	needed := e.neededStructs()

	// needed only contains structs proven to exist (neededStructs filters on
	// reg.Structs != nil), and the requested commands were validated above,
	// so the lookups below are total.
	for _, name := range needed {
		e.emitInputStruct(&b, e.reg.Structs[name])
	}
	for _, name := range needed {
		if err := e.emitStructEncoder(&b, e.reg.Structs[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.commands {
		if err := e.emitCommandEncoder(&b, e.reg.Commands[name]); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// neededStructs returns the transitive set of structs to emit: the requested
// ones plus any nested struct types they (or the requested commands)
// reference, in a stable order (referenced-before-referencing so the file
// reads top-down).
func (e *Emitter) neededStructs() []string {
	want := map[string]bool{}
	var order []string
	var visit func(name string)
	visit = func(name string) {
		if want[name] || e.reg.Structs[name] == nil {
			return
		}
		want[name] = true
		for _, m := range e.reg.Structs[name].Members {
			if e.reg.Structs[m.Type] != nil {
				visit(m.Type)
			}
		}
		order = append(order, name)
	}
	for _, name := range e.structs {
		visit(name)
	}
	for _, name := range e.commands {
		// Commands are validated to exist before neededStructs is called.
		for _, p := range e.reg.Commands[name].Params {
			if e.reg.Structs[p.Type] != nil {
				visit(p.Type)
			}
		}
	}
	return order
}

func (e *Emitter) emitInputStruct(b *bytes.Buffer, s *Struct) {
	fmt.Fprintf(b, "// %s is the pure-Go input for the generated Encode%s\n", s.Name, s.Name)
	fmt.Fprintf(b, "// encoder. sType and pNext are not fields: sType is fixed by the\n")
	fmt.Fprintf(b, "// struct identity and pNext is NULL in M0.\n")
	fmt.Fprintf(b, "type %s struct {\n", s.Name)
	for _, m := range s.Members {
		if m.IsSType || m.IsPNext {
			continue
		}
		fmt.Fprintf(b, "\t%s %s\n", exportName(m.Name), goTypeOf(m))
	}
	b.WriteString("}\n\n")
}

func (e *Emitter) emitStructEncoder(b *bytes.Buffer, s *Struct) error {
	sTypeToken := ""
	for _, m := range s.Members {
		if m.IsSType {
			sTypeToken = m.STypeValue
		}
	}
	if sTypeToken == "" {
		return fmt.Errorf("gen: struct %q has no sType member; M0 only emits sType-bearing structs", s.Name)
	}
	val := e.reg.EnumValues[sTypeToken]
	if val == "" {
		return fmt.Errorf("gen: no enum value for %q", sTypeToken)
	}

	fmt.Fprintf(b, "// Encode%s encodes a %s onto enc, following Mesa\n", s.Name, s.Name)
	fmt.Fprintf(b, "// vn_encode_%s: sType (int32) + pNext (simple_pointer NULL)\n", s.Name)
	fmt.Fprintf(b, "// + self members in declaration order.\n")
	fmt.Fprintf(b, "func Encode%s(enc *vncs.Encoder, v *%s) {\n", s.Name, s.Name)
	fmt.Fprintf(b, "\tenc.EncodeInt32(%s) // sType = %s\n", val, sTypeToken)
	fmt.Fprintf(b, "\tenc.EncodeSimplePointer(false) // pNext = NULL (M0)\n")
	for _, m := range s.Members {
		if m.IsSType || m.IsPNext {
			continue
		}
		if err := e.emitMember(b, "v", m); err != nil {
			return err
		}
	}
	b.WriteString("}\n\n")
	return nil
}

// emitMember emits the encode call(s) for one struct member, choosing the
// vncs primitive from the member's type and attributes exactly as Mesa's
// generator does.
func (e *Emitter) emitMember(b *bytes.Buffer, recv string, m *Member) error {
	field := recv + "." + exportName(m.Name)
	switch {
	case m.PointerToConst: // const char* const*, e.g. ppEnabledLayerNames
		// Mesa: if (names) { array_size(count); for each { string } }
		//       else array_size(0).
		// The count member is encoded separately as its own uint32 member,
		// so here we only emit the names block, guarded on len != 0.
		fmt.Fprintf(b, "\tif len(%s) != 0 {\n", field)
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(uint64(len(%s)))\n", field)
		fmt.Fprintf(b, "\t\tfor _, s := range %s {\n", field)
		fmt.Fprintf(b, "\t\t\tenc.EncodeString(s)\n")
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
		return nil
	case m.Type == "char" && m.Pointer: // const char*, e.g. pApplicationName
		// Mesa: if (s) { array_size(strlen+1); char_array } else array_size(0)
		fmt.Fprintf(b, "\tif %s != \"\" {\n", field)
		fmt.Fprintf(b, "\t\tenc.EncodeString(%s)\n", field)
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
		return nil
	case m.Pointer && e.reg.Structs[m.Type] != nil: // pointer to nested struct
		// Mesa: if (vn_encode_simple_pointer(enc, p)) vn_encode_X(enc, p);
		fmt.Fprintf(b, "\tif enc.EncodeSimplePointer(%s != nil) {\n", field)
		fmt.Fprintf(b, "\t\tEncode%s(enc, %s)\n", m.Type, field)
		fmt.Fprintf(b, "\t}\n")
		return nil
	}
	// Scalar members.
	switch m.Type {
	case "uint32_t":
		fmt.Fprintf(b, "\tenc.EncodeUint32(%s)\n", field)
	case "int32_t":
		fmt.Fprintf(b, "\tenc.EncodeInt32(%s)\n", field)
	case "uint64_t":
		fmt.Fprintf(b, "\tenc.EncodeUint64(%s)\n", field)
	case "float":
		fmt.Fprintf(b, "\tenc.EncodeFloat32(%s)\n", field)
	case "VkFlags", "VkInstanceCreateFlags":
		fmt.Fprintf(b, "\tenc.EncodeFlags(%s)\n", field)
	default:
		return fmt.Errorf("gen: unsupported member %s of type %q (M0 proof subset)", m.Name, m.Type)
	}
	return nil
}

func (e *Emitter) emitCommandEncoder(b *bytes.Buffer, c *Command) error {
	cmdToken := "VK_COMMAND_TYPE_" + c.Name + "_EXT"
	val, ok := e.commandTypeValue(c.Name)
	if !ok {
		return fmt.Errorf("gen: no VkCommandTypeEXT value for %q", c.Name)
	}

	// Determine which params are encodable in M0: a leading pointer param
	// to a known struct (pCreateInfo) or to a handle (pInstance). pAllocator
	// is always NULL in Venus (Mesa asserts(false) if non-NULL).
	var sig []string
	for _, p := range c.Params {
		switch {
		case p.Type == "VkAllocationCallbacks":
			// no Go arg; always NULL
		case e.reg.Structs[p.Type] != nil:
			sig = append(sig, fmt.Sprintf("%s *%s", lowerName(p.Name), p.Type))
		default: // handle out-param such as pInstance
			sig = append(sig, fmt.Sprintf("%s uint64", lowerName(p.Name)))
		}
	}

	fmt.Fprintf(b, "// Encode_%s frames the %s command: VkCommandTypeEXT\n", c.Name, c.Name)
	fmt.Fprintf(b, "// (int32 = %s) + cmdFlags (VkFlags) + each arg as an\n", cmdToken)
	fmt.Fprintf(b, "// optional simple_pointer followed by its pointee, per Mesa\n")
	fmt.Fprintf(b, "// vn_encode_%s.\n", c.Name)
	fmt.Fprintf(b, "func Encode_%s(enc *vncs.Encoder, cmdFlags uint32, %s) {\n", c.Name, strings.Join(sig, ", "))
	fmt.Fprintf(b, "\tenc.EncodeInt32(%s) // cmd_type = %s\n", val, cmdToken)
	fmt.Fprintf(b, "\tenc.EncodeFlags(cmdFlags)\n")
	for _, p := range c.Params {
		arg := lowerName(p.Name)
		switch {
		case p.Type == "VkAllocationCallbacks":
			fmt.Fprintf(b, "\tenc.EncodeSimplePointer(false) // %s = NULL (Venus asserts non-NULL is unreachable)\n", p.Name)
		case e.reg.Structs[p.Type] != nil:
			fmt.Fprintf(b, "\tif enc.EncodeSimplePointer(%s != nil) {\n", arg)
			fmt.Fprintf(b, "\t\tEncode%s(enc, %s)\n", p.Type, arg)
			fmt.Fprintf(b, "\t}\n")
		default: // handle: encoded only if present; id 0 means NULL handle
			fmt.Fprintf(b, "\tif enc.EncodeSimplePointer(%s != 0) {\n", arg)
			fmt.Fprintf(b, "\t\tenc.EncodeHandle(%s)\n", arg)
			fmt.Fprintf(b, "\t}\n")
		}
	}
	b.WriteString("}\n\n")
	return nil
}

// commandTypeValue resolves the integer value of VK_COMMAND_TYPE_<cmd>_EXT.
// This enum lives in the Venus protocol (vn_protocol_driver_defines.h), not
// in vk.xml; for M0 the proof subset's values are pinned here from that
// header (vkCreateInstance = 0) so the generator stays offline and
// vk.xml-only for everything else.
func (e *Emitter) commandTypeValue(cmd string) (string, bool) {
	v, ok := venusCommandTypeValues[cmd]
	return v, ok
}

// venusCommandTypeValues pins VkCommandTypeEXT ordinals transcribed from
// Mesa src/virtio/venus-protocol/vn_protocol_driver_defines.h.
var venusCommandTypeValues = map[string]string{
	"vkCreateInstance": "0", // VK_COMMAND_TYPE_vkCreateInstance_EXT = 0
}

// exportName upper-cases the first rune of a member name for the Go field.
func exportName(name string) string {
	if name == "" {
		return name
	}
	return strings.ToUpper(name[:1]) + name[1:]
}

// lowerName lower-cases the first rune of a param name for a Go parameter.
func lowerName(name string) string {
	if name == "" {
		return name
	}
	return strings.ToLower(name[:1]) + name[1:]
}

func fileHeader(pkg string) string {
	return "" +
		"// Code generated by cmd/vkgen from vk.xml; DO NOT EDIT.\n" +
		"//\n" +
		"// These encoders implement the Mesa Venus wire format (see package\n" +
		"// internal/vncs for the per-primitive Mesa citations).\n\n" +
		"package " + pkg + "\n\n" +
		"import \"github.com/go-virtio/venus/internal/vncs\"\n\n"
}

// SortedStructNames is a small helper kept for tooling/tests that want a
// deterministic listing of a registry's structs.
func SortedStructNames(r *Registry) []string {
	names := r.StructNames()
	sort.Strings(names)
	return names
}
