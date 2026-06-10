// Command vkgen parses a Vulkan registry (vk.xml) and emits the Venus
// proof-set encoders for go-virtio/venus Milestone 0.
//
// Usage:
//
//	vkgen -xml path/to/vk.xml -out proof/encoders_gen.go
//
// The struct/command set is the M0 proof subset (VkApplicationInfo,
// VkInstanceCreateInfo, vkCreateInstance). The generator itself is general
// (see package gen); the subset is a CLI input, not a hardcoded path.
package main

import (
	"flag"
	"fmt"
	"go/format"
	"os"

	"github.com/go-virtio/venus/gen"
)

// proofStructs / proofCommands / proofReplies define the proof subset. Only
// top-level structs are listed; nested structs they reference (e.g.
// VkApplicationInfo, VkExtent3D, VkClearColorValue) are pulled in
// transitively by the emitter. proofReplies is the subset of commands for
// which a reply decoder is also emitted (create-style commands whose reply
// returns a single dispatchable handle).
var (
	proofStructs = []string{
		"VkInstanceCreateInfo",
		"VkDeviceCreateInfo",
		"VkImageCreateInfo",
		"VkMemoryAllocateInfo",
		"VkCommandPoolCreateInfo",
		"VkCommandBufferAllocateInfo",
		"VkCommandBufferBeginInfo",
		"VkImageSubresourceRange",
		"VkClearColorValue",
		"VkSubmitInfo",
		"VkImageMemoryBarrier",
	}
	proofCommands = []string{
		"vkCreateInstance",
		"vkEnumeratePhysicalDevices",
		"vkGetPhysicalDeviceMemoryProperties",
		"vkGetPhysicalDeviceQueueFamilyProperties",
		"vkCreateDevice",
		"vkGetDeviceQueue",
		"vkCreateImage",
		"vkGetImageMemoryRequirements",
		"vkAllocateMemory",
		"vkBindImageMemory",
		"vkCreateCommandPool",
		"vkAllocateCommandBuffers",
		"vkBeginCommandBuffer",
		"vkEndCommandBuffer",
		"vkCmdClearColorImage",
		"vkQueueSubmit",
		"vkCmdPipelineBarrier",
		"vkQueueWaitIdle",
		"vkWaitForFences",
	}
	proofReplies = []string{
		"vkCreateInstance",
		"vkCreateDevice",
		"vkCreateImage",
		"vkAllocateMemory",
		"vkCreateCommandPool",
	}
	// proofCountArrayReplies is the subset of commands whose reply is a
	// uint32 out-count + a counted handle array (vkEnumeratePhysicalDevices).
	proofCountArrayReplies = []string{
		"vkEnumeratePhysicalDevices",
	}
	// proofDecodeStructs is the set of returned-only structs decoded on the
	// reply/readback side. Nested structs are pulled in transitively.
	proofDecodeStructs = []string{
		"VkMemoryRequirements",
		"VkPhysicalDeviceProperties",
		"VkPhysicalDeviceMemoryProperties",
		"VkQueueFamilyProperties",
	}
	// proofPartialStructs is the set of returned-only structs a Get*-query
	// encodes as a _partial skeleton on the request side.
	proofPartialStructs = []string{
		"VkMemoryRequirements",
		"VkPhysicalDeviceMemoryProperties",
		"VkQueueFamilyProperties",
	}
	// proofResultReplies are commands whose reply is just cmd-echo + VkResult.
	proofResultReplies = []string{
		"vkBindImageMemory",
		"vkBeginCommandBuffer",
		"vkEndCommandBuffer",
		"vkQueueWaitIdle",
		"vkWaitForFences",
	}
	// proofVoidHandleReplies are void commands whose reply is a single handle
	// behind a simple_pointer (vkGetDeviceQueue).
	proofVoidHandleReplies = []string{
		"vkGetDeviceQueue",
	}
	// proofStructReplies are void Get*-queries whose reply decodes a single
	// returned struct behind a simple_pointer.
	proofStructReplies = []string{
		"vkGetImageMemoryRequirements",
		"vkGetPhysicalDeviceMemoryProperties",
	}
	// proofCountStructArrayReplies are void queries whose reply is a uint32
	// count + a counted struct array (vkGetPhysicalDeviceQueueFamilyProperties).
	proofCountStructArrayReplies = []string{
		"vkGetPhysicalDeviceQueueFamilyProperties",
	}
	// proofCountHandleArrayStructReplies are commands whose reply is a VkResult
	// + a counted handle array whose count comes from a struct field
	// (vkAllocateCommandBuffers).
	proofCountHandleArrayStructReplies = []string{
		"vkAllocateCommandBuffers",
	}
	// proofPNextChains is the set of sType structs whose encoder emits a real
	// pNext extension chain (VkSubmitInfo / VkImageMemoryBarrier on the
	// clear-image submit/barrier path).
	proofPNextChains = []string{
		"VkSubmitInfo",
		"VkImageMemoryBarrier",
	}
	// proofPNextNodes is the set of extension-node structs used in a pNext
	// chain on the submit/barrier path; each gets a self-encoder + a node
	// constructor. VkProtectedSubmitInfo chains onto VkSubmitInfo.
	proofPNextNodes = []string{
		"VkProtectedSubmitInfo",
	}
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "vkgen:", err)
		os.Exit(1)
	}
}

func run() error {
	fs := flag.NewFlagSet("vkgen", flag.ContinueOnError)
	xmlPath := fs.String("xml", "vk.xml", "path to the Vulkan registry vk.xml")
	outPath := fs.String("out", "proof/encoders_gen.go", "output Go file")
	pkg := fs.String("pkg", "proof", "output package name")
	if err := fs.Parse(os.Args[1:]); err != nil {
		return err
	}

	data, err := os.ReadFile(*xmlPath)
	if err != nil {
		return fmt.Errorf("read %s: %w", *xmlPath, err)
	}
	src, err := Generate(data, *pkg, proofSets())
	if err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, src, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *outPath, err)
	}
	fmt.Fprintf(os.Stderr, "vkgen: wrote %s (%d bytes)\n", *outPath, len(src))
	return nil
}

// ProofSet bundles every struct/command/reply/decode subset the generator is
// driven with, so the (now sizeable) configuration travels as one value instead
// of a dozen positional slices.
type ProofSet struct {
	Structs                     []string
	Commands                    []string
	Replies                     []string
	CountArrayReplies           []string
	DecodeStructs               []string
	PNextChains                 []string
	PNextNodes                  []string
	PartialStructs              []string
	ResultReplies               []string
	VoidHandleReplies           []string
	StructReplies               []string
	CountStructArrayReplies     []string
	CountHandleArrayStructReply []string
}

// proofSets returns the M0 proof subset configuration.
func proofSets() ProofSet {
	return ProofSet{
		Structs:                     proofStructs,
		Commands:                    proofCommands,
		Replies:                     proofReplies,
		CountArrayReplies:           proofCountArrayReplies,
		DecodeStructs:               proofDecodeStructs,
		PNextChains:                 proofPNextChains,
		PNextNodes:                  proofPNextNodes,
		PartialStructs:              proofPartialStructs,
		ResultReplies:               proofResultReplies,
		VoidHandleReplies:           proofVoidHandleReplies,
		StructReplies:               proofStructReplies,
		CountStructArrayReplies:     proofCountStructArrayReplies,
		CountHandleArrayStructReply: proofCountHandleArrayStructReplies,
	}
}

// Generate parses data and emits gofmt'd Go source for the given subset.
// Exposed (not just inlined in run) so tests can exercise the full
// parse->emit->format pipeline without touching the filesystem.
func Generate(data []byte, pkg string, ps ProofSet) ([]byte, error) {
	reg, err := gen.Parse(data)
	if err != nil {
		return nil, err
	}
	raw, err := gen.NewEmitter(reg, ps.Structs, ps.Commands).
		WithReplies(ps.Replies).
		WithCountArrayReplies(ps.CountArrayReplies).
		WithDecodeStructs(ps.DecodeStructs).
		WithPNextChains(ps.PNextChains).
		WithPNextNodes(ps.PNextNodes).
		WithPartialStructs(ps.PartialStructs).
		WithResultReplies(ps.ResultReplies).
		WithVoidHandleReplies(ps.VoidHandleReplies).
		WithStructReplies(ps.StructReplies).
		WithCountStructArrayReplies(ps.CountStructArrayReplies).
		WithCountHandleArrayStructReplies(ps.CountHandleArrayStructReply).
		Generate(pkg)
	if err != nil {
		return nil, err
	}
	src, err := formatSource(raw)
	if err != nil {
		return nil, fmt.Errorf("gofmt generated source: %w\n%s", err, raw)
	}
	return src, nil
}

// formatSource is the gofmt step, behind a variable so tests can inject a
// formatter that fails on otherwise-valid emitter output.
var formatSource = format.Source
