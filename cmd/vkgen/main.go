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
		"VkImageSubresourceRange",
		"VkClearColorValue",
		"VkSubmitInfo",
		"VkImageMemoryBarrier",
	}
	proofCommands = []string{
		"vkCreateInstance",
		"vkEnumeratePhysicalDevices",
		"vkCreateDevice",
		"vkCreateImage",
		"vkAllocateMemory",
		"vkCreateCommandPool",
		"vkCmdClearColorImage",
		"vkQueueSubmit",
		"vkCmdPipelineBarrier",
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
	src, err := Generate(data, *pkg, proofStructs, proofCommands, proofReplies, proofCountArrayReplies, proofDecodeStructs, proofPNextChains, proofPNextNodes)
	if err != nil {
		return err
	}
	if err := os.WriteFile(*outPath, src, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", *outPath, err)
	}
	fmt.Fprintf(os.Stderr, "vkgen: wrote %s (%d bytes)\n", *outPath, len(src))
	return nil
}

// Generate parses data and emits gofmt'd Go source for the given subset.
// Exposed (not just inlined in run) so tests can exercise the full
// parse->emit->format pipeline without touching the filesystem. replies is
// the subset of commands for which a single-handle reply decoder is emitted;
// countArrayReplies is the subset with a count+handle-array reply; and
// decodeStructs is the set of returned-only structs decoded on readback.
func Generate(data []byte, pkg string, structs, commands, replies, countArrayReplies, decodeStructs, pNextChains, pNextNodes []string) ([]byte, error) {
	reg, err := gen.Parse(data)
	if err != nil {
		return nil, err
	}
	raw, err := gen.NewEmitter(reg, structs, commands).
		WithReplies(replies).
		WithCountArrayReplies(countArrayReplies).
		WithDecodeStructs(decodeStructs).
		WithPNextChains(pNextChains).
		WithPNextNodes(pNextNodes).
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
