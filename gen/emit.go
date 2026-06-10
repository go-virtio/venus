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
	// replies is the subset of commands for which a Decode_<cmd>_reply is
	// also emitted (commands whose reply carries a created handle).
	replies []string
	// countArrayReplies is the subset of commands for which a count+array
	// reply decoder is emitted (a uint32 out-count + a counted handle array,
	// e.g. vkEnumeratePhysicalDevices).
	countArrayReplies []string
	// decodeStructs is the set of returned-only structs for which a full
	// Decode<Struct> is emitted (e.g. VkMemoryRequirements). Their nested
	// structs are pulled in transitively, same as the encode side.
	decodeStructs []string
	// pNextChains is the set of sType structs whose encoder emits a real
	// pNext extension chain (EncodePNextChain over a PNext input field)
	// instead of the always-NULL simple_pointer. Membership is looked up via
	// pNextChainSet.
	pNextChains   []string
	pNextChainSet map[string]bool
	// pNextNodes is the set of sType structs that appear as pNext extension
	// nodes: for each, a self-only encoder (Encode<Struct>Self) and a
	// PNextNode constructor (<Struct>Node) are emitted so callers can build a
	// chain. Looked up via pNextNodeSet.
	pNextNodes   []string
	pNextNodeSet map[string]bool
	// partialStructs is the set of returned-only structs that a query command
	// encodes as a "_partial" skeleton on the REQUEST side (Mesa
	// vn_encode_<Struct>_partial): scalars/handles/enums/flags are skipped, but
	// nested struct members (and fixed-array-of-struct members, with their
	// array_size(N) prefix) are still walked and emitted as their own _partial.
	// For each, an Encode<Struct>Partial is emitted and pulled in transitively.
	// Looked up via partialStructSet.
	partialStructs   []string
	partialStructSet map[string]bool
	// voidReplies, resultReplies, structReplies, countStructArrayReplies and
	// countHandleArrayStructLenReplies are the reply-decoder shape buckets for
	// the readback commands (see the With* setters for the exact Mesa shape each
	// transcribes).
	voidHandleReplies           []string
	resultReplies               []string
	structReplies               []string
	countStructArrayReplies     []string
	countHandleArrayStructReply []string
}

// NewEmitter builds an Emitter over reg for the named structs and commands.
func NewEmitter(reg *Registry, structs, commands []string) *Emitter {
	return &Emitter{reg: reg, structs: structs, commands: commands}
}

// WithReplies sets the commands for which a reply decoder is also emitted and
// returns the receiver for chaining. Each name must also appear in the
// command set passed to NewEmitter.
func (e *Emitter) WithReplies(replies []string) *Emitter {
	e.replies = replies
	return e
}

// WithCountArrayReplies sets the commands for which a count+array reply
// decoder is emitted (the uint32-out-count + counted-handle-array reply shape,
// e.g. vkEnumeratePhysicalDevices). Each name must also appear in the command
// set passed to NewEmitter.
func (e *Emitter) WithCountArrayReplies(cmds []string) *Emitter {
	e.countArrayReplies = cmds
	return e
}

// WithDecodeStructs sets the returned-only structs for which a full
// Decode<Struct> is emitted. Nested structs they reference are pulled in
// transitively and decoders emitted for them too.
func (e *Emitter) WithDecodeStructs(structs []string) *Emitter {
	e.decodeStructs = structs
	return e
}

// WithPNextChains marks the sType structs whose encoder emits a real pNext
// extension chain: the input struct gains a `PNext []vncs.PNextNode` field and
// the encoder calls EncodePNextChain(v.PNext) where the always-NULL form would
// have called EncodeSimplePointer(false). Each name must be (or be pulled in
// as) an emitted struct.
func (e *Emitter) WithPNextChains(structs []string) *Emitter {
	e.pNextChains = structs
	e.pNextChainSet = map[string]bool{}
	for _, s := range structs {
		e.pNextChainSet[s] = true
	}
	return e
}

// hasPNextChain reports whether struct name emits a real pNext chain.
func (e *Emitter) hasPNextChain(name string) bool {
	return e.pNextChainSet[name]
}

// WithPartialStructs marks the returned-only structs for which an
// Encode<Struct>Partial is emitted (the request-side skeleton a Get*-query
// sends). Membership is looked up via isPartial; nested struct members are
// pulled into the partial set transitively.
func (e *Emitter) WithPartialStructs(structs []string) *Emitter {
	e.partialStructs = structs
	e.partialStructSet = map[string]bool{}
	for _, s := range structs {
		e.partialStructSet[s] = true
	}
	return e
}

// isPartial reports whether struct name emits an Encode<Struct>Partial.
func (e *Emitter) isPartial(name string) bool { return e.partialStructSet[name] }

// WithVoidHandleReplies sets the commands whose reply is a single dispatchable
// handle behind a simple_pointer with NO VkResult (a void command, e.g.
// vkGetDeviceQueue). Mesa vn_decode_<cmd>_reply: cmd-type echo + simple_pointer
// + handle.
func (e *Emitter) WithVoidHandleReplies(cmds []string) *Emitter {
	e.voidHandleReplies = cmds
	return e
}

// WithResultReplies sets the commands whose reply is just the cmd-type echo +
// VkResult with no out-param (e.g. vkBindImageMemory / vkBeginCommandBuffer /
// vkEndCommandBuffer / vkQueueWaitIdle / vkWaitForFences). Mesa
// vn_decode_<cmd>_reply returns VkResult after skipping every in-param.
func (e *Emitter) WithResultReplies(cmds []string) *Emitter {
	e.resultReplies = cmds
	return e
}

// WithStructReplies sets the commands whose reply decodes a single returned
// struct behind a simple_pointer with NO VkResult (a void Get*-query, e.g.
// vkGetImageMemoryRequirements / vkGetPhysicalDeviceMemoryProperties). The
// decoded struct is the command's pointer-to-returned-struct out-param; a full
// Decode<Struct> must be emitted for it (WithDecodeStructs).
func (e *Emitter) WithStructReplies(cmds []string) *Emitter {
	e.structReplies = cmds
	return e
}

// WithCountStructArrayReplies sets the commands whose reply is a uint32
// out-count + a peeked counted STRUCT array with NO VkResult (a void query,
// e.g. vkGetPhysicalDeviceQueueFamilyProperties). Like the count+handle-array
// shape but the array elements are decoded structs.
func (e *Emitter) WithCountStructArrayReplies(cmds []string) *Emitter {
	e.countStructArrayReplies = cmds
	return e
}

// WithCountHandleArrayStructReplies sets the commands whose reply is a VkResult
// + a peeked counted HANDLE array whose count comes from a struct field (Mesa
// `pAllocateInfo ? pAllocateInfo->commandBufferCount : 0`, e.g.
// vkAllocateCommandBuffers). The array param's len= names a "<struct>-><field>"
// path rather than a sibling count param.
func (e *Emitter) WithCountHandleArrayStructReplies(cmds []string) *Emitter {
	e.countHandleArrayStructReply = cmds
	return e
}

// WithPNextNodes marks the sType structs that appear as pNext extension nodes.
// For each, a self-only encoder (Encode<Struct>Self, the node's members in
// declaration order, no sType/pNext) and a PNextNode constructor
// (<Struct>Node) are emitted so a caller can assemble a chain.
func (e *Emitter) WithPNextNodes(structs []string) *Emitter {
	e.pNextNodes = structs
	e.pNextNodeSet = map[string]bool{}
	for _, s := range structs {
		e.pNextNodeSet[s] = true
	}
	return e
}

// isFlags reports whether a Vulkan type is a VkFlags bitmask typedef
// (VkFlags itself or any Vk*Flags / Vk*FlagBits alias). All encode as a
// 4-byte uint32 (Mesa vn_encode_VkFlags -> vn_encode_uint32_t).
func isFlags(t string) bool {
	return t == "VkFlags" ||
		(strings.HasPrefix(t, "Vk") && strings.HasSuffix(t, "Flags")) ||
		(strings.HasPrefix(t, "Vk") && strings.HasSuffix(t, "FlagBits"))
}

// goTypeOf maps a Vulkan member type onto the Go parameter type used by the
// generated encoder's input struct fields. reg is consulted for enum/handle
// classification (enums -> int32, handles -> uint64 object id).
func goTypeOf(reg *Registry, m *Member) string {
	switch {
	case m.PointerToConst: // const char* const*
		return "[]string"
	case m.Type == "char" && m.Pointer:
		return "string"
	case m.IsPNext:
		return "" // pNext is not a generated field
	case m.FixedArrayLen != "" && m.Type == "char":
		return "string" // fixed char[N] surfaced as a Go string (NUL-padded)
	case m.FixedArrayLen != "" && m.Type == "uint8_t":
		return "[]byte" // fixed uint8[N] (UUID-style)
	case m.FixedArrayLen != "" && m.Type == "uint32_t":
		return "[" + fixedArrayDim(reg, m.FixedArrayLen) + "]uint32" // fixed uint32[N]
	case m.FixedArrayLen != "" && m.Type == "int32_t":
		return "[" + fixedArrayDim(reg, m.FixedArrayLen) + "]int32" // fixed int32[N]
	case m.FixedArrayLen != "" && m.Type == "float":
		return "[" + fixedArrayDim(reg, m.FixedArrayLen) + "]float32" // fixed float[N]
	case m.FixedArrayLen != "" && reg != nil && reg.Structs[m.Type] != nil:
		// fixed array of nested structs (e.g. VkPhysicalDeviceMemoryProperties.
		// memoryTypes[VK_MAX_MEMORY_TYPES]).
		return "[" + fixedArrayDim(reg, m.FixedArrayLen) + "]" + m.Type
	}
	// Counted pointer = a Go slice. Checked before the scalar switch so a
	// `const float*`/`const uint32_t*` array member is a slice, not a scalar.
	if m.Pointer && m.Len != "" {
		switch m.Type {
		case "float":
			return "[]float32"
		case "uint32_t":
			return "[]uint32"
		case "int32_t":
			return "[]int32"
		}
		if reg != nil && reg.Structs[m.Type] != nil {
			return "[]" + m.Type
		}
		if reg != nil && reg.Handles[m.Type] {
			return "[]uint64" // counted handle array (e.g. pWaitSemaphores)
		}
		if isFlags(m.Type) {
			return "[]uint32" // counted VkFlags array (e.g. pWaitDstStageMask)
		}
	}
	switch m.Type {
	case "uint32_t":
		return "uint32"
	case "int32_t":
		return "int32"
	case "uint64_t":
		return "uint64"
	case "size_t":
		return "uint64" // size_t is serialised as uint64 (vn_encode/decode_size_t)
	case "float":
		return "float32"
	case "VkBool32":
		return "bool"
	case "VkDeviceSize", "VkDeviceAddress":
		return "uint64"
	case "VkStructureType":
		return "" // synthesised from the known sType value
	}
	if isFlags(m.Type) {
		return "uint32"
	}
	if reg != nil && reg.EnumTypes[m.Type] {
		return "int32" // enum encoded as int32
	}
	if reg != nil && reg.Handles[m.Type] {
		return "uint64" // handle object id
	}
	if m.Pointer {
		// Pointer to a nested struct, e.g. pApplicationInfo. Represented as
		// a Go pointer to the nested input struct.
		return "*" + m.Type
	}
	// A nested struct/union embedded by value (e.g. VkExtent3D,
	// VkClearColorValue).
	return m.Type
}

// fixedArrayDim resolves a fixed-array dimension token to its Go array-length
// literal (a VK_*_SIZE constant via the registry, or a bare integer). Used
// only for numeric fixed arrays surfaced as Go [N]T values (the union arms).
func fixedArrayDim(reg *Registry, tok string) string {
	if reg != nil {
		if v, ok := reg.EnumValues[tok]; ok {
			return v
		}
	}
	return tok
}

// scalarArrayPrim returns the vncs array primitive name for a counted scalar
// array element type, or "" if the type is not a supported scalar element.
func scalarArrayPrim(t string) string {
	switch t {
	case "float":
		return "EncodeFloat32Array"
	case "uint32_t":
		return "EncodeUint32Array"
	case "int32_t":
		return "EncodeInt32Array"
	}
	return ""
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
	for _, name := range e.replies {
		if e.reg.Commands[name] == nil {
			return nil, fmt.Errorf("gen: reply command %q not found in registry", name)
		}
	}
	for _, name := range e.countArrayReplies {
		if e.reg.Commands[name] == nil {
			return nil, fmt.Errorf("gen: count-array reply command %q not found in registry", name)
		}
	}
	for _, name := range e.decodeStructs {
		if e.reg.Structs[name] == nil {
			return nil, fmt.Errorf("gen: decode struct %q not found in registry", name)
		}
	}
	for _, name := range e.pNextNodes {
		if e.reg.Structs[name] == nil {
			return nil, fmt.Errorf("gen: pNext node struct %q not found in registry", name)
		}
	}
	for _, name := range e.partialStructs {
		if e.reg.Structs[name] == nil {
			return nil, fmt.Errorf("gen: partial struct %q not found in registry", name)
		}
	}
	for _, set := range [][]string{e.voidHandleReplies, e.resultReplies, e.structReplies, e.countStructArrayReplies, e.countHandleArrayStructReply} {
		for _, name := range set {
			if e.reg.Commands[name] == nil {
				return nil, fmt.Errorf("gen: reply command %q not found in registry", name)
			}
		}
	}

	// Collect the nested structs the requested structs/commands reference so
	// we can emit input types for them too (deterministic, deduplicated).
	needed := e.neededStructs()
	// Decode-only structs (and their nested deps) that aren't already covered
	// by an encoder need their input type emitted too.
	decodeNeeded := e.neededDecodeStructs()
	// Partial structs (and their nested deps) need their input type emitted too
	// if no encode/decode pass already covers them.
	partialNeeded := e.neededPartialStructs()
	seen := map[string]bool{}
	for _, name := range needed {
		seen[name] = true
	}

	// needed only contains structs proven to exist (neededStructs filters on
	// reg.Structs != nil), and the requested commands were validated above,
	// so the lookups below are total.
	for _, name := range needed {
		e.emitInputStruct(&b, e.reg.Structs[name])
	}
	for _, name := range decodeNeeded {
		if !seen[name] {
			e.emitInputStruct(&b, e.reg.Structs[name])
			seen[name] = true
		}
	}
	for _, name := range e.pNextNodes {
		if !seen[name] {
			e.emitInputStruct(&b, e.reg.Structs[name])
			seen[name] = true
		}
	}
	for _, name := range partialNeeded {
		if !seen[name] {
			e.emitInputStruct(&b, e.reg.Structs[name])
			seen[name] = true
		}
	}
	for _, name := range needed {
		if err := e.emitStructEncoder(&b, e.reg.Structs[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range partialNeeded {
		if err := e.emitStructPartialEncoder(&b, e.reg.Structs[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.commands {
		if err := e.emitCommandEncoder(&b, e.reg.Commands[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.replies {
		if err := e.emitCommandReplyDecoder(&b, e.reg.Commands[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.countArrayReplies {
		if err := e.emitCountArrayReplyDecoder(&b, e.reg.Commands[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.resultReplies {
		if err := e.emitResultReplyDecoder(&b, e.reg.Commands[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.voidHandleReplies {
		if err := e.emitVoidHandleReplyDecoder(&b, e.reg.Commands[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.structReplies {
		if err := e.emitStructReplyDecoder(&b, e.reg.Commands[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.countStructArrayReplies {
		if err := e.emitCountStructArrayReplyDecoder(&b, e.reg.Commands[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.countHandleArrayStructReply {
		if err := e.emitCountHandleArrayStructReplyDecoder(&b, e.reg.Commands[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range decodeNeeded {
		if err := e.emitStructDecoder(&b, e.reg.Structs[name]); err != nil {
			return nil, err
		}
	}
	for _, name := range e.pNextNodes {
		if err := e.emitPNextNode(&b, e.reg.Structs[name]); err != nil {
			return nil, err
		}
	}
	return b.Bytes(), nil
}

// emitPNextNode emits, for a pNext extension-node struct, a self-only encoder
// Encode<Struct>Self (the node's members in declaration order, skipping
// sType/pNext — the analogue of Mesa's vn_encode_<Struct>_self) and a
// PNextNode constructor <Struct>Node(v) that pairs the node's sType with that
// self encoder. Callers append <Struct>Node(...) values to a parent's PNext
// field; EncodePNextChain then encodes sp(1)+sType+recurse+self per node.
func (e *Emitter) emitPNextNode(b *bytes.Buffer, s *Struct) error {
	if !hasSType(s) {
		return fmt.Errorf("gen: pNext node %s has no sType (not an extension struct)", s.Name)
	}
	sTypeToken := ""
	for _, m := range s.Members {
		if m.IsSType {
			sTypeToken = m.STypeValue
		}
	}
	val := e.reg.EnumValues[sTypeToken]
	if val == "" {
		return fmt.Errorf("gen: no enum value for %q", sTypeToken)
	}

	fmt.Fprintf(b, "// Encode%sSelf encodes just the self members of a %s\n", s.Name, s.Name)
	fmt.Fprintf(b, "// (no sType/pNext), per Mesa vn_encode_%s_self — used as a pNext node.\n", s.Name)
	fmt.Fprintf(b, "func Encode%sSelf(enc *vncs.Encoder, v *%s) {\n", s.Name, s.Name)
	for _, m := range s.Members {
		if m.IsSType || m.IsPNext {
			continue
		}
		if err := e.emitMember(b, "v", m); err != nil {
			return err
		}
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(b, "// %sNode wraps v as a pNext chain node (sType %s = %s).\n", s.Name, sTypeToken, val)
	fmt.Fprintf(b, "func %sNode(v *%s) vncs.PNextNode {\n", s.Name, s.Name)
	fmt.Fprintf(b, "\treturn vncs.PNextNode{SType: %s, EncodeSelf: func(enc *vncs.Encoder) { Encode%sSelf(enc, v) }}\n", val, s.Name)
	b.WriteString("}\n\n")
	return nil
}

// neededDecodeStructs returns the transitive set of structs to emit decoders
// for: the requested decode structs plus any nested struct types they
// reference, referenced-before-referencing.
func (e *Emitter) neededDecodeStructs() []string {
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
	for _, name := range e.decodeStructs {
		visit(name)
	}
	return order
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
		// A returned-only struct queried by a Get*-command is encoded as its
		// _partial skeleton (emitted by the partial pass), never as a full
		// encoder, so it is excluded from the full-encode set here.
		if e.isPartial(name) {
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

// hasSType reports whether a struct carries an sType member (a full Vulkan
// struct vs a plain nested-by-value aggregate like VkExtent3D).
func hasSType(s *Struct) bool {
	for _, m := range s.Members {
		if m.IsSType {
			return true
		}
	}
	return false
}

func (e *Emitter) emitInputStruct(b *bytes.Buffer, s *Struct) {
	if e.reg.Unions[s.Name] {
		fmt.Fprintf(b, "// %s is the pure-Go input for the generated Encode%s union\n", s.Name, s.Name)
		fmt.Fprintf(b, "// encoder. Tag selects the active arm (0=float32, 1=int32, 2=uint32).\n")
		fmt.Fprintf(b, "type %s struct {\n", s.Name)
		fmt.Fprintf(b, "\tTag uint32\n")
		for _, m := range s.Members {
			fmt.Fprintf(b, "\t%s %s\n", exportName(m.Name), goTypeOf(e.reg, m))
		}
		b.WriteString("}\n\n")
		return
	}
	chain := e.hasPNextChain(s.Name)
	if hasSType(s) {
		fmt.Fprintf(b, "// %s is the pure-Go input for the generated Encode%s\n", s.Name, s.Name)
		if chain {
			fmt.Fprintf(b, "// encoder. sType is fixed by the struct identity; PNext is the\n")
			fmt.Fprintf(b, "// (possibly empty) extension chain, encoded via EncodePNextChain.\n")
		} else {
			fmt.Fprintf(b, "// encoder. sType and pNext are not fields: sType is fixed by the\n")
			fmt.Fprintf(b, "// struct identity and pNext is NULL (no extension chain emitted).\n")
		}
	} else {
		fmt.Fprintf(b, "// %s is the pure-Go input for the generated Encode%s\n", s.Name, s.Name)
		fmt.Fprintf(b, "// encoder (a plain nested-by-value Vulkan struct, no sType).\n")
	}
	fmt.Fprintf(b, "type %s struct {\n", s.Name)
	if chain {
		fmt.Fprintf(b, "\tPNext []vncs.PNextNode\n")
	}
	for _, m := range s.Members {
		if m.IsSType || m.IsPNext {
			continue
		}
		fmt.Fprintf(b, "\t%s %s\n", exportName(m.Name), goTypeOf(e.reg, m))
	}
	b.WriteString("}\n\n")
}

func (e *Emitter) emitStructEncoder(b *bytes.Buffer, s *Struct) error {
	if e.reg.Unions[s.Name] {
		return e.emitUnionEncoder(b, s)
	}
	if !hasSType(s) {
		// Plain nested-by-value struct: no sType, no pNext, just the members
		// in order (Mesa vn_encode_<Struct> for an sType-less struct, e.g.
		// vn_encode_VkExtent3D / vn_encode_VkImageSubresourceRange).
		fmt.Fprintf(b, "// Encode%s encodes a %s onto enc, following Mesa\n", s.Name, s.Name)
		fmt.Fprintf(b, "// vn_encode_%s: members in declaration order (no sType/pNext).\n", s.Name)
		fmt.Fprintf(b, "func Encode%s(enc *vncs.Encoder, v *%s) {\n", s.Name, s.Name)
		for _, m := range s.Members {
			if err := e.emitMember(b, "v", m); err != nil {
				return err
			}
		}
		b.WriteString("}\n\n")
		return nil
	}

	sTypeToken := ""
	for _, m := range s.Members {
		if m.IsSType {
			sTypeToken = m.STypeValue
		}
	}
	val := e.reg.EnumValues[sTypeToken]
	if val == "" {
		return fmt.Errorf("gen: no enum value for %q", sTypeToken)
	}

	chain := e.hasPNextChain(s.Name)
	fmt.Fprintf(b, "// Encode%s encodes a %s onto enc, following Mesa\n", s.Name, s.Name)
	if chain {
		fmt.Fprintf(b, "// vn_encode_%s: sType (int32) + the pNext chain (vn_encode_%s_pnext)\n", s.Name, s.Name)
		fmt.Fprintf(b, "// + self members in declaration order.\n")
	} else {
		fmt.Fprintf(b, "// vn_encode_%s: sType (int32) + pNext (simple_pointer NULL)\n", s.Name)
		fmt.Fprintf(b, "// + self members in declaration order.\n")
	}
	fmt.Fprintf(b, "func Encode%s(enc *vncs.Encoder, v *%s) {\n", s.Name, s.Name)
	fmt.Fprintf(b, "\tenc.EncodeInt32(%s) // sType = %s\n", val, sTypeToken)
	if chain {
		fmt.Fprintf(b, "\tenc.EncodePNextChain(v.PNext) // pNext extension chain\n")
	} else {
		fmt.Fprintf(b, "\tenc.EncodeSimplePointer(false) // pNext = NULL\n")
	}
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

// neededPartialStructs returns the transitive set of structs to emit _partial
// encoders for: the requested partial structs plus any nested struct types they
// reference (referenced-before-referencing), since Mesa's _partial walk recurses
// into nested struct/array-of-struct members as their own _partial.
func (e *Emitter) neededPartialStructs() []string {
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
	for _, name := range e.partialStructs {
		visit(name)
	}
	return order
}

// emitStructPartialEncoder emits Encode<Struct>Partial, transcribed from Mesa
// vn_encode_<Struct>_partial: every scalar/handle/enum/flags/fixed-numeric-array
// member is SKIPPED (the renderer fills it in), while nested struct members are
// recursed as their own _partial and fixed-array-of-struct members keep their
// array_size(N) prefix followed by N element _partials. This is the skeleton a
// Get*-query sends on the request side.
func (e *Emitter) emitStructPartialEncoder(b *bytes.Buffer, s *Struct) error {
	fmt.Fprintf(b, "// Encode%sPartial encodes the request-side _partial skeleton of a\n", s.Name)
	fmt.Fprintf(b, "// %s, following Mesa vn_encode_%s_partial: scalar members are\n", s.Name, s.Name)
	fmt.Fprintf(b, "// skipped; nested-struct and fixed-struct-array members are walked.\n")
	fmt.Fprintf(b, "func Encode%sPartial(enc *vncs.Encoder, v *%s) {\n", s.Name, s.Name)
	for _, m := range s.Members {
		if m.IsSType || m.IsPNext {
			continue
		}
		if err := e.emitMemberPartial(b, "v", m); err != nil {
			return err
		}
	}
	b.WriteString("}\n\n")
	return nil
}

// emitMemberPartial emits the _partial encode of one member: only nested
// struct-by-value and fixed-array-of-struct members produce output (everything
// else is skipped, exactly as Mesa's vn_encode_<Struct>_partial comments out the
// scalar members).
func (e *Emitter) emitMemberPartial(b *bytes.Buffer, recv string, m *Member) error {
	if m.IsSType || m.IsPNext {
		return nil
	}
	field := recv + "." + exportName(m.Name)
	switch {
	case m.FixedArrayLen != "" && e.reg.Structs[m.Type] != nil:
		// Fixed-array-of-struct (e.g. VkPhysicalDeviceMemoryProperties.memoryTypes
		// [VK_MAX_MEMORY_TYPES]): array_size(N) then N element _partials.
		n := e.fixedArrayN(m.FixedArrayLen)
		if n == "" {
			return fmt.Errorf("gen: partial member %s fixed-array length %q has no known value", m.Name, m.FixedArrayLen)
		}
		fmt.Fprintf(b, "\tenc.EncodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\tfor i := range %s {\n", field)
		fmt.Fprintf(b, "\t\tEncode%sPartial(enc, &%s[i])\n", m.Type, field)
		fmt.Fprintf(b, "\t}\n")
	case !m.Pointer && m.FixedArrayLen == "" && e.reg.Structs[m.Type] != nil:
		// Nested struct by value (e.g. VkQueueFamilyProperties.minImageTransfer-
		// Granularity): recurse as its own _partial.
		fmt.Fprintf(b, "\tEncode%sPartial(enc, &%s)\n", m.Type, field)
	default:
		// Scalar/handle/enum/flags/fixed-numeric-array: skipped in _partial.
	}
	return nil
}

// emitUnionEncoder emits a tagged-union encoder, transcribed from Mesa
// vn_encode_VkClearColorValue_tag:
//
//	vn_encode_uint32_t(enc, &tag);
//	switch (tag) {
//	case 0: array_size(4); float_array(val->float32, 4); break;
//	case 1: array_size(4); int32_array(val->int32, 4);  break;
//	case 2: array_size(4); uint32_array(val->uint32, 4); break;
//	}
//
// The generator handles a 4-element numeric union (the VkClearColorValue
// shape). Each member is one arm keyed by its declaration index, which is
// the union selector value Mesa uses.
func (e *Emitter) emitUnionEncoder(b *bytes.Buffer, s *Struct) error {
	fmt.Fprintf(b, "// Encode%s encodes the %s union onto enc, following Mesa\n", s.Name, s.Name)
	fmt.Fprintf(b, "// vn_encode_%s_tag: uint32 tag + array_size(4) + the 4-element arm.\n", s.Name)
	fmt.Fprintf(b, "func Encode%s(enc *vncs.Encoder, v *%s) {\n", s.Name, s.Name)
	fmt.Fprintf(b, "\tenc.EncodeUint32(v.Tag)\n")
	fmt.Fprintf(b, "\tenc.EncodeArraySize(4)\n")
	fmt.Fprintf(b, "\tswitch v.Tag {\n")
	for i, m := range s.Members {
		var prim string
		switch m.Type {
		case "float":
			prim = "EncodeFloat32Array"
		case "int32_t":
			prim = "EncodeInt32Array"
		case "uint32_t":
			prim = "EncodeUint32Array"
		default:
			return fmt.Errorf("gen: union %s arm %s has unsupported type %q (only 4-element float/int32/uint32 arms are emitted)", s.Name, m.Name, m.Type)
		}
		fmt.Fprintf(b, "\tcase %d:\n", i)
		fmt.Fprintf(b, "\t\tenc.%s(v.%s[:])\n", prim, exportName(m.Name))
	}
	fmt.Fprintf(b, "\t}\n")
	b.WriteString("}\n\n")
	return nil
}

// emitMember emits the encode call(s) for one struct member, choosing the
// vncs primitive from the member's type and attributes exactly as Mesa's
// generator does.
func (e *Emitter) emitMember(b *bytes.Buffer, recv string, m *Member) error {
	if m.IsSType || m.IsPNext {
		return nil
	}
	field := recv + "." + exportName(m.Name)
	switch {
	case m.PointerToConst: // const char* const*, e.g. ppEnabledLayerNames
		// Mesa: if (names) { array_size(count); for each { string } }
		//       else array_size(0).
		fmt.Fprintf(b, "\tif len(%s) != 0 {\n", field)
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(uint64(len(%s)))\n", field)
		fmt.Fprintf(b, "\t\tfor _, s := range %s {\n", field)
		fmt.Fprintf(b, "\t\t\tenc.EncodeString(s)\n")
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
		return nil
	case m.FixedArrayLen != "":
		return e.emitFixedArrayMember(b, field, m)
	case m.Type == "char" && m.Pointer: // const char*, e.g. pApplicationName
		fmt.Fprintf(b, "\tif %s != \"\" {\n", field)
		fmt.Fprintf(b, "\t\tenc.EncodeString(%s)\n", field)
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
		return nil
	case m.Pointer && m.Len != "" && e.reg.Structs[m.Type] != nil:
		// Counted pointer to an array of structs inside a struct (e.g.
		// VkDeviceCreateInfo.pQueueCreateInfos). Mesa:
		//   if (p) { array_size(count); for {...} } else array_size(0).
		fmt.Fprintf(b, "\tif len(%s) != 0 {\n", field)
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(uint64(len(%s)))\n", field)
		fmt.Fprintf(b, "\t\tfor i := range %s {\n", field)
		fmt.Fprintf(b, "\t\t\tEncode%s(enc, &%s[i])\n", m.Type, field)
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
		return nil
	case m.Pointer && m.Len != "" && scalarArrayPrim(m.Type) != "":
		// Counted pointer to a scalar array inside a struct (e.g.
		// VkDeviceQueueCreateInfo.pQueuePriorities = const float*,
		// VkImageCreateInfo.pQueueFamilyIndices = const uint32_t*). Mesa:
		//   if (p) { array_size(count); T_array(p, count) } else array_size(0).
		fmt.Fprintf(b, "\tif len(%s) != 0 {\n", field)
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(uint64(len(%s)))\n", field)
		fmt.Fprintf(b, "\t\tenc.%s(%s)\n", scalarArrayPrim(m.Type), field)
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
		return nil
	case m.Pointer && m.Len != "" && e.reg.Handles[m.Type]:
		// Counted pointer to a handle array inside a struct (e.g.
		// VkSubmitInfo.pWaitSemaphores/pCommandBuffers/pSignalSemaphores).
		// Mesa: if (p) { array_size(count); for { handle } } else array_size(0).
		fmt.Fprintf(b, "\tif len(%s) != 0 {\n", field)
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(uint64(len(%s)))\n", field)
		fmt.Fprintf(b, "\t\tfor i := range %s {\n", field)
		fmt.Fprintf(b, "\t\t\tenc.EncodeHandle(%s[i])\n", field)
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
		return nil
	case m.Pointer && m.Len != "" && isFlags(m.Type):
		// Counted pointer to a VkFlags array inside a struct (e.g.
		// VkSubmitInfo.pWaitDstStageMask, whose len is waitSemaphoreCount).
		// Mesa: if (p) { array_size(count); for { VkFlags } } else array_size(0).
		fmt.Fprintf(b, "\tif len(%s) != 0 {\n", field)
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(uint64(len(%s)))\n", field)
		fmt.Fprintf(b, "\t\tfor i := range %s {\n", field)
		fmt.Fprintf(b, "\t\t\tenc.EncodeFlags(%s[i])\n", field)
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
		return nil
	case m.Pointer && e.reg.Structs[m.Type] != nil: // pointer to nested struct
		fmt.Fprintf(b, "\tif enc.EncodeSimplePointer(%s != nil) {\n", field)
		fmt.Fprintf(b, "\t\tEncode%s(enc, %s)\n", m.Type, field)
		fmt.Fprintf(b, "\t}\n")
		return nil
	case !m.Pointer && e.reg.Structs[m.Type] != nil: // nested struct/union by value
		fmt.Fprintf(b, "\tEncode%s(enc, &%s)\n", m.Type, field)
		return nil
	}
	// Scalar members.
	switch {
	case m.Type == "uint32_t":
		fmt.Fprintf(b, "\tenc.EncodeUint32(%s)\n", field)
	case m.Type == "int32_t":
		fmt.Fprintf(b, "\tenc.EncodeInt32(%s)\n", field)
	case m.Type == "uint64_t":
		fmt.Fprintf(b, "\tenc.EncodeUint64(%s)\n", field)
	case m.Type == "float":
		fmt.Fprintf(b, "\tenc.EncodeFloat32(%s)\n", field)
	case m.Type == "VkBool32":
		fmt.Fprintf(b, "\tenc.EncodeBool32(%s)\n", field)
	case m.Type == "VkDeviceSize" || m.Type == "VkDeviceAddress":
		fmt.Fprintf(b, "\tenc.EncodeDeviceSize(%s)\n", field)
	case isFlags(m.Type):
		fmt.Fprintf(b, "\tenc.EncodeFlags(%s)\n", field)
	case e.reg.EnumTypes[m.Type]:
		fmt.Fprintf(b, "\tenc.EncodeInt32(%s) // enum %s\n", field, m.Type)
	case e.reg.Handles[m.Type]:
		fmt.Fprintf(b, "\tenc.EncodeHandle(%s) // handle %s\n", field, m.Type)
	default:
		return fmt.Errorf("gen: unsupported member %s of type %q", m.Name, m.Type)
	}
	return nil
}

// emitFixedArrayMember emits a fixed-size array member, transcribed from
// Mesa's struct encoders for char[N] / uint8[N] / uint32[N] fields, each of
// which is array_size(N) followed by the N-element blob/array:
//
//	size += vn_sizeof_array_size(VK_UUID_SIZE);
//	size += vn_sizeof_uint8_t_array(val->pipelineCacheUUID, VK_UUID_SIZE);
//
// N comes from the registry's API-constant table (VK_UUID_SIZE -> 16) or a
// literal. Multi-dimensional arrays are refused.
func (e *Emitter) emitFixedArrayMember(b *bytes.Buffer, field string, m *Member) error {
	if strings.Contains(m.FixedArrayLen, "[multidim]") {
		return fmt.Errorf("gen: member %s is a multi-dimensional fixed array (%q), not supported", m.Name, m.FixedArrayLen)
	}
	n := e.fixedArrayN(m.FixedArrayLen)
	if n == "" {
		return fmt.Errorf("gen: member %s fixed-array length %q has no known value", m.Name, m.FixedArrayLen)
	}
	switch m.Type {
	case "char":
		// char[N]: array_size(N) + blob_array of the NUL-padded string.
		fmt.Fprintf(b, "\tenc.EncodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\t{\n")
		fmt.Fprintf(b, "\t\tbuf := make([]byte, %s)\n", n)
		fmt.Fprintf(b, "\t\tcopy(buf, %s)\n", field)
		fmt.Fprintf(b, "\t\tenc.EncodeBlobArray(buf)\n")
		fmt.Fprintf(b, "\t}\n")
	case "uint8_t":
		fmt.Fprintf(b, "\tenc.EncodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\tenc.EncodeUint8Array(%s)\n", field)
	case "uint32_t":
		fmt.Fprintf(b, "\tenc.EncodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\tenc.EncodeUint32Array(%s)\n", field)
	case "float":
		fmt.Fprintf(b, "\tenc.EncodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\tenc.EncodeFloat32Array(%s)\n", field)
	default:
		return fmt.Errorf("gen: fixed array of %q (member %s) not supported", m.Type, m.Name)
	}
	return nil
}

// fixedArrayN resolves a fixed-array dimension token to its integer literal:
// a VK_*_SIZE API constant via the registry, or a bare integer passed
// through. Returns "" if unknown.
func (e *Emitter) fixedArrayN(tok string) string {
	if v, ok := e.reg.EnumValues[tok]; ok {
		return v
	}
	for _, r := range tok {
		if r < '0' || r > '9' {
			return ""
		}
	}
	if tok == "" {
		return ""
	}
	return tok
}

func (e *Emitter) emitCommandEncoder(b *bytes.Buffer, c *Command) error {
	cmdToken := "VK_COMMAND_TYPE_" + c.Name + "_EXT"
	val, ok := e.commandTypeValue(c.Name)
	if !ok {
		return fmt.Errorf("gen: no VkCommandTypeEXT value for %q", c.Name)
	}

	sig, err := e.commandSignature(c)
	if err != nil {
		return err
	}

	fmt.Fprintf(b, "// Encode_%s frames the %s command: VkCommandTypeEXT\n", c.Name, c.Name)
	fmt.Fprintf(b, "// (int32 = %s) + cmdFlags (VkFlags) + the encoded params,\n", cmdToken)
	fmt.Fprintf(b, "// per Mesa vn_encode_%s.\n", c.Name)
	fmt.Fprintf(b, "func Encode_%s(enc *vncs.Encoder, cmdFlags uint32%s) {\n", c.Name, sig)
	fmt.Fprintf(b, "\tenc.EncodeInt32(%s) // cmd_type = %s\n", val, cmdToken)
	fmt.Fprintf(b, "\tenc.EncodeFlags(cmdFlags)\n")
	for _, p := range c.Params {
		// commandSignature has already validated every param above, so
		// emitParam cannot fail here; it asserts that invariant.
		e.emitParam(b, p)
	}
	b.WriteString("}\n\n")
	return nil
}

// commandSignature builds the Go parameter list (after cmdFlags) for a
// command encoder, classifying each param.
func (e *Emitter) commandSignature(c *Command) (string, error) {
	var sig []string
	for _, p := range c.Params {
		arg := lowerName(p.Name)
		switch {
		case p.Type == "VkAllocationCallbacks":
			// no Go arg; always NULL
		case p.Pointer && p.Len != "" && (e.reg.Structs[p.Type] != nil || e.reg.Handles[p.Type]):
			// counted array param, e.g. pRanges / pSubmits / pPhysicalDevices.
			if e.reg.Handles[p.Type] {
				sig = append(sig, fmt.Sprintf("%s []uint64", arg))
			} else {
				sig = append(sig, fmt.Sprintf("%s []%s", arg, p.Type))
			}
		case p.Pointer && e.reg.Structs[p.Type] != nil:
			sig = append(sig, fmt.Sprintf("%s *%s", arg, p.Type))
		case p.Pointer && e.reg.Handles[p.Type]:
			// out handle (e.g. pInstance) or in handle pointer.
			sig = append(sig, fmt.Sprintf("%s uint64", arg))
		case !p.Pointer && e.reg.Handles[p.Type]:
			sig = append(sig, fmt.Sprintf("%s uint64", arg))
		case !p.Pointer && e.reg.EnumTypes[p.Type]:
			sig = append(sig, fmt.Sprintf("%s int32", arg))
		case !p.Pointer && p.Type == "uint32_t":
			sig = append(sig, fmt.Sprintf("%s uint32", arg))
		case !p.Pointer && p.Type == "uint64_t":
			// by-value uint64 (e.g. vkWaitForFences.timeout).
			sig = append(sig, fmt.Sprintf("%s uint64", arg))
		case !p.Pointer && p.Type == "VkBool32":
			// by-value VkBool32 (e.g. vkWaitForFences.waitAll).
			sig = append(sig, fmt.Sprintf("%s bool", arg))
		case !p.Pointer && p.Type == "VkDeviceSize":
			// by-value VkDeviceSize (e.g. vkBindImageMemory.memoryOffset).
			sig = append(sig, fmt.Sprintf("%s uint64", arg))
		case !p.Pointer && isFlags(p.Type):
			sig = append(sig, fmt.Sprintf("%s uint32", arg))
		case p.Pointer && p.Type == "uint32_t":
			// out count pointer (e.g. pPhysicalDeviceCount) — request side
			// passes it by value; encoded as a simple_pointer + uint32.
			sig = append(sig, fmt.Sprintf("%s uint32", arg))
		default:
			return "", fmt.Errorf("gen: command %s param %s of type %q (pointer=%v len=%q) not supported", c.Name, p.Name, p.Type, p.Pointer, p.Len)
		}
	}
	if len(sig) == 0 {
		return "", nil
	}
	return ", " + strings.Join(sig, ", "), nil
}

// emitParam emits the encode call(s) for one command param, following the
// per-param shape in Mesa's vn_encode_<command>. It must only be called after
// commandSignature has accepted the param; an unclassifiable param is a
// generator invariant violation and panics (commandSignature returns an error
// for that case instead).
func (e *Emitter) emitParam(b *bytes.Buffer, p *Param) {
	arg := lowerName(p.Name)
	switch {
	case p.Type == "VkAllocationCallbacks":
		// Mesa: if (vn_encode_simple_pointer(enc, pAllocator)) assert(false);
		fmt.Fprintf(b, "\tenc.EncodeSimplePointer(false) // %s = NULL (Venus asserts non-NULL is unreachable)\n", p.Name)
	case p.Pointer && p.Len != "" && e.reg.Structs[p.Type] != nil:
		// Counted array of structs: uint32 count is a separate param; here
		// Mesa emits `if (p) { array_size(count); for {...} } else array_size(0)`.
		// For a returned-only struct queried by a Get*-command the element is
		// encoded as its _partial skeleton (Mesa vn_encode_<Struct>_partial).
		enc := "Encode" + p.Type
		if e.isPartial(p.Type) {
			enc += "Partial"
		}
		fmt.Fprintf(b, "\tif len(%s) != 0 {\n", arg)
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(uint64(len(%s)))\n", arg)
		fmt.Fprintf(b, "\t\tfor i := range %s {\n", arg)
		fmt.Fprintf(b, "\t\t\t%s(enc, &%s[i])\n", enc, arg)
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
	case p.Pointer && p.Len != "" && e.reg.Handles[p.Type] && strings.Contains(p.Len, "->"):
		// Counted handle out-array whose element count comes from a struct field
		// (Mesa `pAllocateInfo ? pAllocateInfo->commandBufferCount : 0`, e.g.
		// vkAllocateCommandBuffers.pCommandBuffers). Presence is the array
		// pointer; the encoded count is the struct field, not the slice length.
		structArg, field := splitStructFieldLen(p.Len)
		fmt.Fprintf(b, "\tif len(%s) != 0 {\n", arg)
		fmt.Fprintf(b, "\t\tvar n uint32\n")
		fmt.Fprintf(b, "\t\tif %s != nil {\n", structArg)
		fmt.Fprintf(b, "\t\t\tn = %s.%s\n", structArg, exportName(field))
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(uint64(n))\n")
		fmt.Fprintf(b, "\t\tfor i := uint32(0); i < n; i++ {\n")
		fmt.Fprintf(b, "\t\t\tenc.EncodeHandle(%s[i])\n", arg)
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
	case p.Pointer && p.Len != "" && e.reg.Handles[p.Type]:
		fmt.Fprintf(b, "\tif len(%s) != 0 {\n", arg)
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(uint64(len(%s)))\n", arg)
		fmt.Fprintf(b, "\t\tfor i := range %s {\n", arg)
		fmt.Fprintf(b, "\t\t\tenc.EncodeHandle(%s[i])\n", arg)
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t} else {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeArraySize(0)\n")
		fmt.Fprintf(b, "\t}\n")
	case p.Pointer && e.reg.Structs[p.Type] != nil:
		// Optional pointer to a struct, e.g. pCreateInfo / pColor. A returned-only
		// struct queried by a Get*-command is encoded as its _partial skeleton
		// (Mesa vn_encode_<Struct>_partial), e.g. vkGetImageMemoryRequirements's
		// pMemoryRequirements.
		enc := "Encode" + p.Type
		if e.isPartial(p.Type) {
			enc += "Partial"
		}
		fmt.Fprintf(b, "\tif enc.EncodeSimplePointer(%s != nil) {\n", arg)
		fmt.Fprintf(b, "\t\t%s(enc, %s)\n", enc, arg)
		fmt.Fprintf(b, "\t}\n")
	case p.Pointer && e.reg.Handles[p.Type]:
		// Out handle (e.g. pInstance): encoded only if present (id 0 = NULL).
		fmt.Fprintf(b, "\tif enc.EncodeSimplePointer(%s != 0) {\n", arg)
		fmt.Fprintf(b, "\t\tenc.EncodeHandle(%s)\n", arg)
		fmt.Fprintf(b, "\t}\n")
	case p.Pointer && p.Type == "uint32_t":
		// Out count pointer (request side sends the current value).
		fmt.Fprintf(b, "\tif enc.EncodeSimplePointer(true) {\n")
		fmt.Fprintf(b, "\t\tenc.EncodeUint32(%s)\n", arg)
		fmt.Fprintf(b, "\t}\n")
	case !p.Pointer && e.reg.Handles[p.Type]:
		fmt.Fprintf(b, "\tenc.EncodeHandle(%s) // handle %s\n", arg, p.Type)
	case !p.Pointer && e.reg.EnumTypes[p.Type]:
		fmt.Fprintf(b, "\tenc.EncodeInt32(%s) // enum %s\n", arg, p.Type)
	case !p.Pointer && p.Type == "uint32_t":
		fmt.Fprintf(b, "\tenc.EncodeUint32(%s)\n", arg)
	case !p.Pointer && p.Type == "uint64_t":
		fmt.Fprintf(b, "\tenc.EncodeUint64(%s)\n", arg)
	case !p.Pointer && p.Type == "VkBool32":
		fmt.Fprintf(b, "\tenc.EncodeBool32(%s)\n", arg)
	case !p.Pointer && p.Type == "VkDeviceSize":
		fmt.Fprintf(b, "\tenc.EncodeDeviceSize(%s)\n", arg)
	case !p.Pointer && isFlags(p.Type):
		fmt.Fprintf(b, "\tenc.EncodeFlags(%s)\n", arg)
	default:
		panic(fmt.Sprintf("gen: emitParam reached an unclassified param %s of type %q; commandSignature should have rejected it", p.Name, p.Type))
	}
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
// Mesa src/virtio/venus-protocol/vn_protocol_driver_defines.h. This enum is a
// Venus-protocol artifact, not part of vk.xml, so the ordinals are kept here.
var venusCommandTypeValues = map[string]string{
	"vkCreateInstance":                         "0",
	"vkEnumeratePhysicalDevices":               "2",
	"vkGetPhysicalDeviceQueueFamilyProperties": "7",
	"vkGetPhysicalDeviceMemoryProperties":      "8",
	"vkCreateDevice":                           "11",
	"vkGetDeviceQueue":                         "17",
	"vkQueueSubmit":                            "18",
	"vkQueueWaitIdle":                          "19",
	"vkAllocateMemory":                         "21",
	"vkMapMemory":                              "23",
	"vkBindImageMemory":                        "29",
	"vkGetImageMemoryRequirements":             "31",
	"vkWaitForFences":                          "39",
	"vkCreateImage":                            "54",
	"vkCreateCommandPool":                      "85",
	"vkAllocateCommandBuffers":                 "88",
	"vkBeginCommandBuffer":                     "90",
	"vkEndCommandBuffer":                       "91",
	"vkCmdClearColorImage":                     "119",
	"vkCmdPipelineBarrier":                     "126",
}

// emitCommandReplyDecoder emits a Decode_<cmd>_reply that mirrors Mesa's
// vn_decode_<cmd>_reply for a "create"-style command whose only reply
// out-param is a single dispatchable handle behind a simple_pointer. The
// reply framing (vn_protocol_driver_instance.h / _device.h) is:
//
//	vn_decode_VkCommandTypeEXT(dec, &command_type);   // echo of the request type
//	assert(command_type == VK_COMMAND_TYPE_<cmd>_EXT);
//	vn_decode_VkResult(dec, &ret);
//	// skip the in-params
//	if (vn_decode_simple_pointer(dec)) vn_decode_VkHandle(dec, pHandle);
//	return ret;
//
// The decoder returns (result int32, handle uint64, ok bool) where ok is the
// simple_pointer presence flag. It also checks the echoed command type, the
// analogue of Mesa's assert.
func (e *Emitter) emitCommandReplyDecoder(b *bytes.Buffer, c *Command) error {
	cmdToken := "VK_COMMAND_TYPE_" + c.Name + "_EXT"
	val, ok := e.commandTypeValue(c.Name)
	if !ok {
		return fmt.Errorf("gen: no VkCommandTypeEXT value for %q", c.Name)
	}
	// The reply out-handle is the last pointer-to-handle param.
	var outHandle *Param
	for _, p := range c.Params {
		if p.Pointer && e.reg.Handles[p.Type] {
			outHandle = p
		}
	}
	if outHandle == nil {
		return fmt.Errorf("gen: command %q has no handle out-param; reply decode for non-create replies is not yet emitted", c.Name)
	}
	out := lowerName(outHandle.Name)

	fmt.Fprintf(b, "// %sReplyCmdType is the VkCommandTypeEXT the %s reply echoes\n", exportName(c.Name), c.Name)
	fmt.Fprintf(b, "// (%s).\n", cmdToken)
	fmt.Fprintf(b, "const %sReplyCmdType int32 = %s\n\n", exportName(c.Name), val)
	fmt.Fprintf(b, "// Decode_%s_reply decodes the %s reply, per Mesa\n", c.Name, c.Name)
	fmt.Fprintf(b, "// vn_decode_%s_reply: command-type echo + VkResult +\n", c.Name)
	fmt.Fprintf(b, "// simple_pointer(%s). ok is the simple_pointer presence flag;\n", outHandle.Name)
	fmt.Fprintf(b, "// cmdType is the echoed command type the caller verifies against\n")
	fmt.Fprintf(b, "// %sReplyCmdType (Mesa asserts the same equality).\n", exportName(c.Name))
	fmt.Fprintf(b, "func Decode_%s_reply(dec *vncs.Decoder) (cmdType int32, result int32, %s uint64, ok bool) {\n", c.Name, out)
	fmt.Fprintf(b, "\tcmdType = dec.DecodeInt32() // echoed %s\n", cmdToken)
	fmt.Fprintf(b, "\tresult = dec.DecodeResult()\n")
	fmt.Fprintf(b, "\tif dec.DecodeSimplePointer() {\n")
	fmt.Fprintf(b, "\t\t%s = dec.DecodeHandle()\n", out)
	fmt.Fprintf(b, "\t\tok = true\n")
	fmt.Fprintf(b, "\t}\n")
	fmt.Fprintf(b, "\treturn cmdType, result, %s, ok\n", out)
	b.WriteString("}\n\n")
	return nil
}

// emitCountArrayReplyDecoder emits a Decode_<cmd>_reply for the
// "count + handle array" reply shape, transcribed from Mesa's
// vn_decode_vkEnumeratePhysicalDevices_reply (vn_protocol_driver_device.h):
//
//	vn_decode_VkCommandTypeEXT(dec, &command_type);
//	assert(command_type == VK_COMMAND_TYPE_<cmd>_EXT);
//	vn_decode_VkResult(dec, &ret);
//	// skip the leading in-param(s) (e.g. instance)
//	if (vn_decode_simple_pointer(dec)) vn_decode_uint32_t(dec, pCount);
//	else pCount = NULL;
//	if (vn_peek_array_size(dec)) {
//	    const uint32_t n = vn_decode_array_size(dec, *pCount);
//	    for (i < n) vn_decode_VkHandle(dec, &pArray[i]);
//	} else {
//	    vn_decode_array_size_unchecked(dec);   // consume the 0
//	    pArray = NULL;
//	}
//	return ret;
//
// The out-count is the pointer-to-uint32 param; the out-array is the counted
// pointer-to-handle param (its len= names the count param). countOK is the
// count's simple_pointer flag; the returned handle slice is nil when the
// peeked array_size is 0.
func (e *Emitter) emitCountArrayReplyDecoder(b *bytes.Buffer, c *Command) error {
	cmdToken := "VK_COMMAND_TYPE_" + c.Name + "_EXT"
	val, ok := e.commandTypeValue(c.Name)
	if !ok {
		return fmt.Errorf("gen: no VkCommandTypeEXT value for %q", c.Name)
	}
	var countParam, arrayParam *Param
	for _, p := range c.Params {
		switch {
		case p.Pointer && p.Type == "uint32_t":
			countParam = p
		case p.Pointer && p.Len != "" && e.reg.Handles[p.Type]:
			arrayParam = p
		}
	}
	if countParam == nil || arrayParam == nil {
		return fmt.Errorf("gen: command %q is not a count+array reply shape (count=%v array=%v)", c.Name, countParam, arrayParam)
	}
	countName := lowerName(countParam.Name)
	arrayName := lowerName(arrayParam.Name)

	fmt.Fprintf(b, "// %sReplyCmdType is the VkCommandTypeEXT the %s reply echoes\n", exportName(c.Name), c.Name)
	fmt.Fprintf(b, "// (%s).\n", cmdToken)
	fmt.Fprintf(b, "const %sReplyCmdType int32 = %s\n\n", exportName(c.Name), val)
	fmt.Fprintf(b, "// Decode_%s_reply decodes the %s reply, per Mesa\n", c.Name, c.Name)
	fmt.Fprintf(b, "// vn_decode_%s_reply: command-type echo + VkResult +\n", c.Name)
	fmt.Fprintf(b, "// simple_pointer(%s)+uint32 + peeked counted %s handle array.\n", countParam.Name, arrayParam.Name)
	fmt.Fprintf(b, "// countOK is the count's simple_pointer flag; %s is nil when the\n", arrayName)
	fmt.Fprintf(b, "// peeked array_size is 0 (the array_size(0) is still consumed, matching\n")
	fmt.Fprintf(b, "// vn_decode_array_size_unchecked).\n")
	fmt.Fprintf(b, "func Decode_%s_reply(dec *vncs.Decoder) (cmdType int32, result int32, %s uint32, countOK bool, %s []uint64) {\n", c.Name, countName, arrayName)
	fmt.Fprintf(b, "\tcmdType = dec.DecodeInt32() // echoed %s\n", cmdToken)
	fmt.Fprintf(b, "\tresult = dec.DecodeResult()\n")
	fmt.Fprintf(b, "\tif dec.DecodeSimplePointer() {\n")
	fmt.Fprintf(b, "\t\t%s = dec.DecodeUint32()\n", countName)
	fmt.Fprintf(b, "\t\tcountOK = true\n")
	fmt.Fprintf(b, "\t}\n")
	fmt.Fprintf(b, "\tif dec.PeekArraySize() != 0 {\n")
	fmt.Fprintf(b, "\t\tn := dec.DecodeArraySize(uint64(%s))\n", countName)
	fmt.Fprintf(b, "\t\t%s = make([]uint64, n)\n", arrayName)
	fmt.Fprintf(b, "\t\tfor i := range %s {\n", arrayName)
	fmt.Fprintf(b, "\t\t\t%s[i] = dec.DecodeHandle()\n", arrayName)
	fmt.Fprintf(b, "\t\t}\n")
	fmt.Fprintf(b, "\t} else {\n")
	fmt.Fprintf(b, "\t\tdec.DecodeArraySizeUnchecked() // consume the array_size(0)\n")
	fmt.Fprintf(b, "\t}\n")
	fmt.Fprintf(b, "\treturn cmdType, result, %s, countOK, %s\n", countName, arrayName)
	b.WriteString("}\n\n")
	return nil
}

// replyHeader emits the const + the cmd-type echo decode shared by every reply
// decoder, returning the resolved VkCommandTypeEXT ordinal value.
func (e *Emitter) replyHeader(b *bytes.Buffer, c *Command) (string, error) {
	cmdToken := "VK_COMMAND_TYPE_" + c.Name + "_EXT"
	val, ok := e.commandTypeValue(c.Name)
	if !ok {
		return "", fmt.Errorf("gen: no VkCommandTypeEXT value for %q", c.Name)
	}
	fmt.Fprintf(b, "// %sReplyCmdType is the VkCommandTypeEXT the %s reply echoes\n", exportName(c.Name), c.Name)
	fmt.Fprintf(b, "// (%s).\n", cmdToken)
	fmt.Fprintf(b, "const %sReplyCmdType int32 = %s\n\n", exportName(c.Name), val)
	return cmdToken, nil
}

// emitResultReplyDecoder emits a Decode_<cmd>_reply for a "result only" reply
// (Mesa vn_decode_<cmd>_reply for vkBindImageMemory / vkBeginCommandBuffer /
// vkEndCommandBuffer / vkQueueWaitIdle / vkWaitForFences):
//
//	vn_decode_VkCommandTypeEXT(dec, &command_type);
//	assert(command_type == VK_COMMAND_TYPE_<cmd>_EXT);
//	vn_decode_VkResult(dec, &ret);
//	/* skip every in-param */
//	return ret;
func (e *Emitter) emitResultReplyDecoder(b *bytes.Buffer, c *Command) error {
	cmdToken, err := e.replyHeader(b, c)
	if err != nil {
		return err
	}
	fmt.Fprintf(b, "// Decode_%s_reply decodes the %s reply, per Mesa\n", c.Name, c.Name)
	fmt.Fprintf(b, "// vn_decode_%s_reply: command-type echo + VkResult (no out-param).\n", c.Name)
	fmt.Fprintf(b, "func Decode_%s_reply(dec *vncs.Decoder) (cmdType int32, result int32) {\n", c.Name)
	fmt.Fprintf(b, "\tcmdType = dec.DecodeInt32() // echoed %s\n", cmdToken)
	fmt.Fprintf(b, "\tresult = dec.DecodeResult()\n")
	fmt.Fprintf(b, "\treturn cmdType, result\n")
	b.WriteString("}\n\n")
	return nil
}

// emitVoidHandleReplyDecoder emits a Decode_<cmd>_reply for a void command whose
// reply carries a single dispatchable handle behind a simple_pointer and NO
// VkResult (Mesa vn_decode_vkGetDeviceQueue_reply):
//
//	vn_decode_VkCommandTypeEXT(dec, &command_type);
//	assert(command_type == VK_COMMAND_TYPE_<cmd>_EXT);
//	/* skip the in-params */
//	if (vn_decode_simple_pointer(dec)) vn_decode_VkHandle(dec, pHandle);
func (e *Emitter) emitVoidHandleReplyDecoder(b *bytes.Buffer, c *Command) error {
	cmdToken, err := e.replyHeader(b, c)
	if err != nil {
		return err
	}
	var outHandle *Param
	for _, p := range c.Params {
		if p.Pointer && e.reg.Handles[p.Type] {
			outHandle = p
		}
	}
	if outHandle == nil {
		return fmt.Errorf("gen: command %q has no handle out-param for a void-handle reply", c.Name)
	}
	out := lowerName(outHandle.Name)
	fmt.Fprintf(b, "// Decode_%s_reply decodes the %s reply, per Mesa\n", c.Name, c.Name)
	fmt.Fprintf(b, "// vn_decode_%s_reply: command-type echo + simple_pointer(%s)\n", c.Name, outHandle.Name)
	fmt.Fprintf(b, "// (no VkResult — %s returns void). ok is the simple_pointer flag.\n", c.Name)
	fmt.Fprintf(b, "func Decode_%s_reply(dec *vncs.Decoder) (cmdType int32, %s uint64, ok bool) {\n", c.Name, out)
	fmt.Fprintf(b, "\tcmdType = dec.DecodeInt32() // echoed %s\n", cmdToken)
	fmt.Fprintf(b, "\tif dec.DecodeSimplePointer() {\n")
	fmt.Fprintf(b, "\t\t%s = dec.DecodeHandle()\n", out)
	fmt.Fprintf(b, "\t\tok = true\n")
	fmt.Fprintf(b, "\t}\n")
	fmt.Fprintf(b, "\treturn cmdType, %s, ok\n", out)
	b.WriteString("}\n\n")
	return nil
}

// emitStructReplyDecoder emits a Decode_<cmd>_reply for a void Get*-query whose
// reply decodes a single returned struct behind a simple_pointer and NO
// VkResult (Mesa vn_decode_vkGetImageMemoryRequirements_reply /
// vn_decode_vkGetPhysicalDeviceMemoryProperties_reply):
//
//	vn_decode_VkCommandTypeEXT(dec, &command_type);
//	assert(command_type == VK_COMMAND_TYPE_<cmd>_EXT);
//	/* skip the in-params */
//	if (vn_decode_simple_pointer(dec)) vn_decode_<Struct>(dec, pStruct);
func (e *Emitter) emitStructReplyDecoder(b *bytes.Buffer, c *Command) error {
	cmdToken, err := e.replyHeader(b, c)
	if err != nil {
		return err
	}
	var outStruct *Param
	for _, p := range c.Params {
		if p.Pointer && p.Len == "" && e.reg.Structs[p.Type] != nil {
			outStruct = p
		}
	}
	if outStruct == nil {
		return fmt.Errorf("gen: command %q has no returned-struct out-param for a struct reply", c.Name)
	}
	out := lowerName(outStruct.Name)
	fmt.Fprintf(b, "// Decode_%s_reply decodes the %s reply, per Mesa\n", c.Name, c.Name)
	fmt.Fprintf(b, "// vn_decode_%s_reply: command-type echo + simple_pointer(%s)\n", c.Name, outStruct.Name)
	fmt.Fprintf(b, "// + Decode%s (no VkResult — %s returns void). ok is the\n", outStruct.Type, c.Name)
	fmt.Fprintf(b, "// simple_pointer flag.\n")
	fmt.Fprintf(b, "func Decode_%s_reply(dec *vncs.Decoder, %s *%s) (cmdType int32, ok bool) {\n", c.Name, out, outStruct.Type)
	fmt.Fprintf(b, "\tcmdType = dec.DecodeInt32() // echoed %s\n", cmdToken)
	fmt.Fprintf(b, "\tif dec.DecodeSimplePointer() {\n")
	fmt.Fprintf(b, "\t\tDecode%s(dec, %s)\n", outStruct.Type, out)
	fmt.Fprintf(b, "\t\tok = true\n")
	fmt.Fprintf(b, "\t}\n")
	fmt.Fprintf(b, "\treturn cmdType, ok\n")
	b.WriteString("}\n\n")
	return nil
}

// emitCountStructArrayReplyDecoder emits a Decode_<cmd>_reply for a void query
// whose reply is a uint32 out-count + a peeked counted STRUCT array and NO
// VkResult (Mesa vn_decode_vkGetPhysicalDeviceQueueFamilyProperties_reply):
//
//	vn_decode_VkCommandTypeEXT(dec, &command_type);
//	assert(command_type == VK_COMMAND_TYPE_<cmd>_EXT);
//	if (vn_decode_simple_pointer(dec)) vn_decode_uint32_t(dec, pCount);
//	if (vn_peek_array_size(dec)) {
//	    const uint32_t n = vn_decode_array_size(dec, *pCount);
//	    for (i < n) vn_decode_<Struct>(dec, &pArray[i]);
//	} else { vn_decode_array_size_unchecked(dec); pArray = NULL; }
func (e *Emitter) emitCountStructArrayReplyDecoder(b *bytes.Buffer, c *Command) error {
	cmdToken, err := e.replyHeader(b, c)
	if err != nil {
		return err
	}
	var countParam, arrayParam *Param
	for _, p := range c.Params {
		switch {
		case p.Pointer && p.Type == "uint32_t":
			countParam = p
		case p.Pointer && p.Len != "" && e.reg.Structs[p.Type] != nil:
			arrayParam = p
		}
	}
	if countParam == nil || arrayParam == nil {
		return fmt.Errorf("gen: command %q is not a count+struct-array reply shape (count=%v array=%v)", c.Name, countParam, arrayParam)
	}
	countName := lowerName(countParam.Name)
	arrayName := lowerName(arrayParam.Name)
	fmt.Fprintf(b, "// Decode_%s_reply decodes the %s reply, per Mesa\n", c.Name, c.Name)
	fmt.Fprintf(b, "// vn_decode_%s_reply: command-type echo + simple_pointer(%s)+uint32\n", c.Name, countParam.Name)
	fmt.Fprintf(b, "// + peeked counted %s struct array (no VkResult — void query).\n", arrayParam.Name)
	fmt.Fprintf(b, "// countOK is the count's simple_pointer flag; %s is nil when the\n", arrayName)
	fmt.Fprintf(b, "// peeked array_size is 0.\n")
	fmt.Fprintf(b, "func Decode_%s_reply(dec *vncs.Decoder) (cmdType int32, %s uint32, countOK bool, %s []%s) {\n", c.Name, countName, arrayName, arrayParam.Type)
	fmt.Fprintf(b, "\tcmdType = dec.DecodeInt32() // echoed %s\n", cmdToken)
	fmt.Fprintf(b, "\tif dec.DecodeSimplePointer() {\n")
	fmt.Fprintf(b, "\t\t%s = dec.DecodeUint32()\n", countName)
	fmt.Fprintf(b, "\t\tcountOK = true\n")
	fmt.Fprintf(b, "\t}\n")
	fmt.Fprintf(b, "\tif dec.PeekArraySize() != 0 {\n")
	fmt.Fprintf(b, "\t\tn := dec.DecodeArraySize(uint64(%s))\n", countName)
	fmt.Fprintf(b, "\t\t%s = make([]%s, n)\n", arrayName, arrayParam.Type)
	fmt.Fprintf(b, "\t\tfor i := range %s {\n", arrayName)
	fmt.Fprintf(b, "\t\t\tDecode%s(dec, &%s[i])\n", arrayParam.Type, arrayName)
	fmt.Fprintf(b, "\t\t}\n")
	fmt.Fprintf(b, "\t} else {\n")
	fmt.Fprintf(b, "\t\tdec.DecodeArraySizeUnchecked() // consume the array_size(0)\n")
	fmt.Fprintf(b, "\t}\n")
	fmt.Fprintf(b, "\treturn cmdType, %s, countOK, %s\n", countName, arrayName)
	b.WriteString("}\n\n")
	return nil
}

// emitCountHandleArrayStructReplyDecoder emits a Decode_<cmd>_reply for a
// VkResult + a peeked counted HANDLE array whose count comes from a struct field
// (Mesa vn_decode_vkAllocateCommandBuffers_reply):
//
//	vn_decode_VkCommandTypeEXT(dec, &command_type);
//	assert(command_type == VK_COMMAND_TYPE_<cmd>_EXT);
//	vn_decode_VkResult(dec, &ret);
//	if (vn_peek_array_size(dec)) {
//	    const uint32_t n = vn_decode_array_size(dec, pAllocateInfo->commandBufferCount);
//	    for (i < n) vn_decode_VkHandle(dec, &pArray[i]);
//	} else { vn_decode_array_size_unchecked(dec); pArray = NULL; }
//	return ret;
//
// The count bound is supplied by the caller (the struct field) as maxCount.
func (e *Emitter) emitCountHandleArrayStructReplyDecoder(b *bytes.Buffer, c *Command) error {
	cmdToken, err := e.replyHeader(b, c)
	if err != nil {
		return err
	}
	var arrayParam *Param
	for _, p := range c.Params {
		if p.Pointer && p.Len != "" && e.reg.Handles[p.Type] && strings.Contains(p.Len, "->") {
			arrayParam = p
		}
	}
	if arrayParam == nil {
		return fmt.Errorf("gen: command %q is not a struct-field-counted handle-array reply shape", c.Name)
	}
	arrayName := lowerName(arrayParam.Name)
	fmt.Fprintf(b, "// Decode_%s_reply decodes the %s reply, per Mesa\n", c.Name, c.Name)
	fmt.Fprintf(b, "// vn_decode_%s_reply: command-type echo + VkResult + peeked counted\n", c.Name)
	fmt.Fprintf(b, "// %s handle array bounded by maxCount (the caller's\n", arrayParam.Name)
	fmt.Fprintf(b, "// %s, i.e. the requested commandBufferCount). %s is nil\n", arrayParam.Len, arrayName)
	fmt.Fprintf(b, "// when the peeked array_size is 0.\n")
	fmt.Fprintf(b, "func Decode_%s_reply(dec *vncs.Decoder, maxCount uint32) (cmdType int32, result int32, %s []uint64) {\n", c.Name, arrayName)
	fmt.Fprintf(b, "\tcmdType = dec.DecodeInt32() // echoed %s\n", cmdToken)
	fmt.Fprintf(b, "\tresult = dec.DecodeResult()\n")
	fmt.Fprintf(b, "\tif dec.PeekArraySize() != 0 {\n")
	fmt.Fprintf(b, "\t\tn := dec.DecodeArraySize(uint64(maxCount))\n")
	fmt.Fprintf(b, "\t\t%s = make([]uint64, n)\n", arrayName)
	fmt.Fprintf(b, "\t\tfor i := range %s {\n", arrayName)
	fmt.Fprintf(b, "\t\t\t%s[i] = dec.DecodeHandle()\n", arrayName)
	fmt.Fprintf(b, "\t\t}\n")
	fmt.Fprintf(b, "\t} else {\n")
	fmt.Fprintf(b, "\t\tdec.DecodeArraySizeUnchecked() // consume the array_size(0)\n")
	fmt.Fprintf(b, "\t}\n")
	fmt.Fprintf(b, "\treturn cmdType, result, %s\n", arrayName)
	b.WriteString("}\n\n")
	return nil
}

// emitStructDecoder emits a Decode<Struct> that mirrors Mesa's
// vn_decode_<Struct>: for a returned-only (no-sType) struct it reads each
// member in declaration order; for an sType struct it reads sType + the pNext
// presence flag first. The returned-struct path (VkMemoryRequirements,
// VkPhysicalDeviceProperties) decodes the FULL struct (Mesa's _partial form is
// the request-side encode skeleton, not the reply decode).
func (e *Emitter) emitStructDecoder(b *bytes.Buffer, s *Struct) error {
	if e.reg.Unions[s.Name] {
		return fmt.Errorf("gen: union %s has no generated decoder (returned unions are out of scope)", s.Name)
	}
	fmt.Fprintf(b, "// Decode%s decodes a %s from dec, following Mesa\n", s.Name, s.Name)
	fmt.Fprintf(b, "// vn_decode_%s: members in declaration order.\n", s.Name)
	fmt.Fprintf(b, "func Decode%s(dec *vncs.Decoder, v *%s) {\n", s.Name, s.Name)
	for _, m := range s.Members {
		if m.IsSType || m.IsPNext {
			continue
		}
		if err := e.emitMemberDecode(b, "v", m); err != nil {
			return err
		}
	}
	b.WriteString("}\n\n")
	return nil
}

// emitMemberDecode emits the decode call(s) for one struct member, the inverse
// of emitMember, choosing the vncs decode primitive from the member's type and
// attributes exactly as Mesa's vn_decode_<Struct> does.
func (e *Emitter) emitMemberDecode(b *bytes.Buffer, recv string, m *Member) error {
	if m.IsSType || m.IsPNext {
		return nil
	}
	field := recv + "." + exportName(m.Name)
	if m.FixedArrayLen != "" && e.reg.Structs[m.Type] != nil {
		// Fixed array of nested structs (e.g. VkPhysicalDeviceMemoryProperties.
		// memoryTypes[VK_MAX_MEMORY_TYPES]). Mesa:
		//   const uint32_t n = vn_decode_array_size(dec, N);
		//   for (i < n) vn_decode_<Elem>(dec, &val->field[i]);
		if strings.Contains(m.FixedArrayLen, "[multidim]") {
			return fmt.Errorf("gen: member %s is a multi-dimensional fixed array (%q), not supported", m.Name, m.FixedArrayLen)
		}
		n := e.fixedArrayN(m.FixedArrayLen)
		if n == "" {
			return fmt.Errorf("gen: member %s fixed-array length %q has no known value", m.Name, m.FixedArrayLen)
		}
		fmt.Fprintf(b, "\t{\n")
		fmt.Fprintf(b, "\t\tn := dec.DecodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\t\tfor i := uint64(0); i < n; i++ {\n")
		fmt.Fprintf(b, "\t\t\tDecode%s(dec, &%s[i])\n", m.Type, field)
		fmt.Fprintf(b, "\t\t}\n")
		fmt.Fprintf(b, "\t}\n")
		return nil
	}
	if m.FixedArrayLen != "" {
		return e.emitFixedArrayMemberDecode(b, field, m)
	}
	if !m.Pointer && e.reg.Structs[m.Type] != nil { // nested struct by value
		fmt.Fprintf(b, "\tDecode%s(dec, &%s)\n", m.Type, field)
		return nil
	}
	switch {
	case m.Type == "uint32_t":
		fmt.Fprintf(b, "\t%s = dec.DecodeUint32()\n", field)
	case m.Type == "int32_t":
		fmt.Fprintf(b, "\t%s = dec.DecodeInt32()\n", field)
	case m.Type == "uint64_t":
		fmt.Fprintf(b, "\t%s = dec.DecodeUint64()\n", field)
	case m.Type == "float":
		fmt.Fprintf(b, "\t%s = dec.DecodeFloat32()\n", field)
	case m.Type == "VkBool32":
		fmt.Fprintf(b, "\t%s = dec.DecodeBool32()\n", field)
	case m.Type == "VkDeviceSize" || m.Type == "VkDeviceAddress":
		fmt.Fprintf(b, "\t%s = dec.DecodeDeviceSize()\n", field)
	case m.Type == "size_t":
		fmt.Fprintf(b, "\t%s = dec.DecodeSizeT()\n", field)
	case isFlags(m.Type):
		fmt.Fprintf(b, "\t%s = dec.DecodeFlags()\n", field)
	case e.reg.EnumTypes[m.Type]:
		fmt.Fprintf(b, "\t%s = dec.DecodeInt32() // enum %s\n", field, m.Type)
	case e.reg.Handles[m.Type]:
		fmt.Fprintf(b, "\t%s = dec.DecodeHandle() // handle %s\n", field, m.Type)
	default:
		return fmt.Errorf("gen: unsupported decode member %s of type %q", m.Name, m.Type)
	}
	return nil
}

// emitFixedArrayMemberDecode emits a fixed-size array member decode, the
// inverse of emitFixedArrayMember: array_size(N) is decoded (bounded by N) and
// the N-element blob/array read back. Mirrors Mesa's
//
//	const size_t array_size = vn_decode_array_size(dec, N);
//	vn_decode_<T>_array(dec, val->field, array_size);
func (e *Emitter) emitFixedArrayMemberDecode(b *bytes.Buffer, field string, m *Member) error {
	if strings.Contains(m.FixedArrayLen, "[multidim]") {
		return fmt.Errorf("gen: member %s is a multi-dimensional fixed array (%q), not supported", m.Name, m.FixedArrayLen)
	}
	n := e.fixedArrayN(m.FixedArrayLen)
	if n == "" {
		return fmt.Errorf("gen: member %s fixed-array length %q has no known value", m.Name, m.FixedArrayLen)
	}
	switch m.Type {
	case "char":
		// char[N]: array_size(N) bounded, then a char_array blob of N bytes
		// surfaced as a Go string (trailing NUL bytes trimmed by the caller's
		// reading convention — we keep the raw N bytes for byte fidelity).
		fmt.Fprintf(b, "\t{\n")
		fmt.Fprintf(b, "\t\tn := dec.DecodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\t\t%s = string(dec.DecodeCharArray(int(n)))\n", field)
		fmt.Fprintf(b, "\t}\n")
	case "uint8_t":
		fmt.Fprintf(b, "\t{\n")
		fmt.Fprintf(b, "\t\tn := dec.DecodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\t\t%s = dec.DecodeUint8Array(int(n))\n", field)
		fmt.Fprintf(b, "\t}\n")
	case "uint32_t":
		fmt.Fprintf(b, "\t{\n")
		fmt.Fprintf(b, "\t\tn := dec.DecodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\t\tcopy(%s[:], dec.DecodeUint32Array(int(n)))\n", field)
		fmt.Fprintf(b, "\t}\n")
	case "int32_t":
		fmt.Fprintf(b, "\t{\n")
		fmt.Fprintf(b, "\t\tn := dec.DecodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\t\tcopy(%s[:], dec.DecodeInt32Array(int(n)))\n", field)
		fmt.Fprintf(b, "\t}\n")
	case "float":
		fmt.Fprintf(b, "\t{\n")
		fmt.Fprintf(b, "\t\tn := dec.DecodeArraySize(%s)\n", n)
		fmt.Fprintf(b, "\t\tcopy(%s[:], dec.DecodeFloat32Array(int(n)))\n", field)
		fmt.Fprintf(b, "\t}\n")
	default:
		return fmt.Errorf("gen: fixed array decode of %q (member %s) not supported", m.Type, m.Name)
	}
	return nil
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

// splitStructFieldLen splits a "struct->field" len= token (e.g.
// "pAllocateInfo->commandBufferCount") into the lower-cased Go arg name of the
// struct param and the raw field name. The arrow is the only delimiter Mesa
// uses for a count drawn from a struct member.
func splitStructFieldLen(lenAttr string) (structArg, field string) {
	i := strings.Index(lenAttr, "->")
	structArg = lowerName(lenAttr[:i])
	field = lenAttr[i+len("->"):]
	return structArg, field
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
