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

// proofStructs / proofCommands define the M0 proof subset. Top-level structs
// only; nested structs they reference (e.g. VkApplicationInfo) are pulled in
// transitively by the emitter.
var (
	proofStructs  = []string{"VkInstanceCreateInfo"}
	proofCommands = []string{"vkCreateInstance"}
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
	src, err := Generate(data, *pkg, proofStructs, proofCommands)
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
// parse->emit->format pipeline without touching the filesystem.
func Generate(data []byte, pkg string, structs, commands []string) ([]byte, error) {
	reg, err := gen.Parse(data)
	if err != nil {
		return nil, err
	}
	raw, err := gen.NewEmitter(reg, structs, commands).Generate(pkg)
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
