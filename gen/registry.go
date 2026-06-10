// Package gen parses the Khronos Vulkan registry (vk.xml) and emits pure-Go
// Venus wire encoders that call the vncs runtime. The encoding it emits
// follows the Mesa venus-protocol rules transcribed in package
// internal/vncs.
//
// Design intent: this is the *real* generator shape, not a hand-rolled
// stub for three structs. registry.go turns the relevant slice of vk.xml
// into a typed model (Type/Struct/Member/Command); emit.go walks that model
// the same way Mesa's python generator walks it — honouring sType/pNext,
// optional=, len=, altlen= and noautovalidity= — to decide, per member,
// which vncs primitive to call. M0 only exercises a small proof subset, but
// adding a new struct is "parse it + add it to the requested set", never a
// new code path.
package gen

import (
	"encoding/xml"
	"fmt"
	"strings"
)

// registryXML is the subset of vk.xml we care about for M0: the <types> and
// <commands> sections. encoding/xml ignores elements we do not model.
type registryXML struct {
	XMLName  xml.Name `xml:"registry"`
	Types    typesXML `xml:"types"`
	Commands struct {
		Commands []commandXML `xml:"command"`
	} `xml:"commands"`
	Enums []enumsXML `xml:"enums"`
}

type typesXML struct {
	Types []typeXML `xml:"type"`
}

type typeXML struct {
	Category string      `xml:"category,attr"`
	Name     string      `xml:"name,attr"`
	NameElem string      `xml:"name"` // <type name="x"> uses attr; struct members use <name>
	Members  []memberXML `xml:"member"`
}

type memberXML struct {
	// Raw mixed content is needed to recover "const char* const*" etc.
	Raw            string `xml:",innerxml"`
	Type           string `xml:"type"`
	Name           string `xml:"name"`
	Optional       string `xml:"optional,attr"`
	Len            string `xml:"len,attr"`
	AltLen         string `xml:"altlen,attr"`
	NoAutoValidity string `xml:"noautovalidity,attr"`
	Values         string `xml:"values,attr"`
}

type commandXML struct {
	Proto struct {
		Type string `xml:"type"`
		Name string `xml:"name"`
	} `xml:"proto"`
	Params []paramXML `xml:"param"`
}

type paramXML struct {
	Raw      string `xml:",innerxml"`
	Type     string `xml:"type"`
	Name     string `xml:"name"`
	Optional string `xml:"optional,attr"`
	Len      string `xml:"len,attr"`
}

type enumsXML struct {
	Name  string    `xml:"name,attr"`
	Type  string    `xml:"type,attr"`
	Enums []enumXML `xml:"enum"`
}

type enumXML struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

// Registry is the parsed, typed model of the relevant slice of vk.xml.
type Registry struct {
	Structs    map[string]*Struct
	Commands   map[string]*Command
	EnumValues map[string]string // enum name -> literal value text (e.g. sType, VK_UUID_SIZE)
	// EnumTypes is the set of Vulkan type names declared as
	// <enums type="enum"> (e.g. VkImageLayout, VkPhysicalDeviceType). Members
	// of these types encode as int32 (vn_encode_int32_t), exactly as Mesa's
	// vn_encode_<EnumType> casts the enum to int32.
	EnumTypes map[string]bool
	// Handles is the set of Vulkan dispatchable/non-dispatchable handle type
	// names (<type category="handle">), e.g. VkImage, VkDevice. A handle
	// member/param encodes as its 8-byte object id (Mesa vn_encode_VkImage et
	// al.; see vncs.Encoder.EncodeHandle).
	Handles map[string]bool
	// Unions is the set of <type category="union"> names (e.g.
	// VkClearColorValue). The emitter encodes a union via its selector tag.
	Unions      map[string]bool
	structOrder []string
}

// Struct models a <type category="struct">.
type Struct struct {
	Name    string
	Members []*Member
}

// Member models one struct <member> with the attributes the encoder needs.
type Member struct {
	Name           string
	Type           string // base Vulkan type, e.g. "uint32_t", "char", "VkApplicationInfo"
	Pointer        bool   // single pointer:  T*
	PointerToConst bool   // T* const* (array of pointers, e.g. ppEnabledLayerNames)
	IsSType        bool   // the sType member (has values=)
	STypeValue     string // the VK_STRUCTURE_TYPE_* token for sType
	IsPNext        bool   // the pNext member
	Optional       bool   // optional="true" (leading token)
	Len            string // len= attribute (e.g. "enabledLayerCount,null-terminated")
	AltLen         string // altlen= attribute
	NoAutoValidity bool   // noautovalidity="true"
	FixedArrayLen  string // [N] dimension after <name>, e.g. "VK_UUID_SIZE" or "3" (empty if scalar)
}

// Command models a <command>.
type Command struct {
	Name   string
	Params []*Param
}

// Param models one command <param>.
type Param struct {
	Name     string
	Type     string
	Pointer  bool
	Optional bool
	Len      string
}

// Parse parses a vk.xml document (the full file or any well-formed subset
// containing <registry><types>/<commands>) into a Registry.
func Parse(data []byte) (*Registry, error) {
	var rx registryXML
	if err := xml.Unmarshal(data, &rx); err != nil {
		return nil, fmt.Errorf("gen: parse vk.xml: %w", err)
	}
	r := &Registry{
		Structs:    map[string]*Struct{},
		Commands:   map[string]*Command{},
		EnumValues: map[string]string{},
		EnumTypes:  map[string]bool{},
		Handles:    map[string]bool{},
		Unions:     map[string]bool{},
	}
	for _, t := range rx.Types.Types {
		// A handle type names itself via a <name> child inside the
		// VK_DEFINE_HANDLE macro text, not the name= attribute.
		if t.Category == "handle" && t.NameElem != "" {
			r.Handles[t.NameElem] = true
			continue
		}
		if t.Category == "union" && t.Name != "" {
			r.Unions[t.Name] = true
			u := &Struct{Name: t.Name}
			for _, m := range t.Members {
				u.Members = append(u.Members, parseMember(m))
			}
			r.Structs[t.Name] = u
			r.structOrder = append(r.structOrder, t.Name)
			continue
		}
		if t.Category != "struct" || t.Name == "" {
			continue
		}
		s := &Struct{Name: t.Name}
		for _, m := range t.Members {
			s.Members = append(s.Members, parseMember(m))
		}
		r.Structs[t.Name] = s
		r.structOrder = append(r.structOrder, t.Name)
	}
	for _, c := range rx.Commands.Commands {
		if c.Proto.Name == "" {
			continue
		}
		cmd := &Command{Name: c.Proto.Name}
		for _, p := range c.Params {
			cmd.Params = append(cmd.Params, parseParam(p))
		}
		r.Commands[c.Proto.Name] = cmd
	}
	for _, es := range rx.Enums {
		if es.Type == "enum" && es.Name != "" {
			// VkImageLayout, VkPhysicalDeviceType, … : members of these types
			// encode as int32 (Mesa vn_encode_<EnumType> casts to int32).
			r.EnumTypes[es.Name] = true
		}
		for _, e := range es.Enums {
			if e.Name != "" && e.Value != "" {
				if _, ok := r.EnumValues[e.Name]; !ok {
					r.EnumValues[e.Name] = e.Value
				}
			}
		}
	}
	return r, nil
}

// StructNames returns struct names in document order (deterministic output).
func (r *Registry) StructNames() []string { return append([]string(nil), r.structOrder...) }

func parseMember(m memberXML) *Member {
	out := &Member{
		Name:           m.Name,
		Type:           m.Type,
		Optional:       leadingOptional(m.Optional),
		Len:            m.Len,
		AltLen:         m.AltLen,
		NoAutoValidity: m.NoAutoValidity == "true",
	}
	classifyPointers(m.Raw, out)
	out.FixedArrayLen = parseFixedArrayLen(m.Raw)
	if m.Values != "" && m.Name == "sType" {
		out.IsSType = true
		out.STypeValue = m.Values
	}
	if m.Name == "pNext" {
		out.IsPNext = true
	}
	return out
}

func parseParam(p paramXML) *Param {
	out := &Param{
		Name:     p.Name,
		Type:     p.Type,
		Optional: leadingOptional(p.Optional),
		Len:      p.Len,
	}
	// A param is a pointer if a '*' appears between the <type> and <name>.
	out.Pointer = strings.Contains(rawBetween(p.Raw), "*")
	return out
}

// leadingOptional decodes the optional= attribute. Vulkan uses a
// comma-separated list where only the first token applies to the member
// itself (subsequent tokens describe deeper indirections). Mesa's generator
// keys struct-member optionality off that leading token.
func leadingOptional(attr string) bool {
	if attr == "" {
		return false
	}
	first := attr
	if i := strings.IndexByte(attr, ','); i >= 0 {
		first = attr[:i]
	}
	return first == "true"
}

// classifyPointers inspects the raw mixed content of a <member> to decide
// whether it is a plain value, a single pointer (T*), or an array-of-const-
// pointers (const T* const*, e.g. ppEnabledLayerNames). The presence of the
// trailing "const*" sequence after the <name>'s preceding tokens marks the
// pointer-to-const-pointer form.
func classifyPointers(raw string, m *Member) {
	between := rawBetween(raw)
	stars := strings.Count(between, "*")
	switch {
	case strings.Contains(collapseSpaces(between), "* const*"):
		m.PointerToConst = true
	case stars >= 1:
		m.Pointer = true
	}
}

// parseFixedArrayLen extracts the fixed-array dimension from the tail of a
// member's inner XML. In vk.xml a fixed array is written as
//
//	<type>char</type> <name>deviceName</name>[<enum>VK_MAX_PHYSICAL_DEVICE_NAME_SIZE</enum>]
//	<type>float</type> <name>matrix</name>[3][4]   // (multi-dim, not modelled)
//
// i.e. one or more "[dim]" suffixes after </name>, where dim is either an
// <enum>NAME</enum> element (a VK_*_SIZE token) or a literal integer. We
// return the single dimension's token text ("VK_UUID_SIZE", "3", …) or ""
// for a scalar. Multi-dimensional arrays return the joined raw so the
// emitter can reject them explicitly rather than silently mis-encode.
func parseFixedArrayLen(raw string) string {
	end := strings.Index(raw, "</name>")
	if end < 0 {
		return ""
	}
	tail := raw[end+len("</name>"):]
	open := strings.IndexByte(tail, '[')
	if open < 0 {
		return ""
	}
	close := strings.IndexByte(tail, ']')
	if close < 0 || close < open {
		return ""
	}
	inner := tail[open+1 : close]
	// Recover the dimension whether it is <enum>NAME</enum> or a literal.
	inner = strings.ReplaceAll(inner, "<enum>", "")
	inner = strings.ReplaceAll(inner, "</enum>", "")
	dim := strings.TrimSpace(inner)
	// Flag a second "[" (multi-dimensional) by appending the remainder so the
	// emitter's fixed-array handler can detect and refuse it.
	if rest := tail[close+1:]; strings.Contains(rest, "[") {
		dim += "[multidim]"
	}
	return dim
}

// rawBetween returns the text between the closing </type> and the opening
// <name> in a member's inner XML, where the pointer punctuation lives.
func rawBetween(raw string) string {
	start := strings.Index(raw, "</type>")
	if start < 0 {
		return ""
	}
	start += len("</type>")
	end := strings.Index(raw, "<name>")
	if end < 0 || end < start {
		return raw[start:]
	}
	return raw[start:end]
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
