package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// withArgs runs fn with os.Args temporarily set to args.
func withArgs(t *testing.T, args []string, fn func()) {
	t.Helper()
	saved := os.Args
	os.Args = args
	defer func() { os.Args = saved }()
	fn()
}

const miniXML = `<registry>
  <types>
    <type category="struct" name="VkApplicationInfo">
      <member values="VK_STRUCTURE_TYPE_APPLICATION_INFO"><type>VkStructureType</type> <name>sType</name></member>
      <member optional="true">const <type>void</type>* <name>pNext</name></member>
      <member><type>uint32_t</type> <name>apiVersion</name></member>
    </type>
    <type category="struct" name="VkInstanceCreateInfo">
      <member values="VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO"><type>VkStructureType</type> <name>sType</name></member>
      <member optional="true">const <type>void</type>* <name>pNext</name></member>
      <member optional="true"><type>VkInstanceCreateFlags</type> <name>flags</name></member>
      <member optional="true">const <type>VkApplicationInfo</type>* <name>pApplicationInfo</name></member>
      <member optional="true"><type>uint32_t</type> <name>enabledLayerCount</name></member>
      <member len="enabledLayerCount,null-terminated">const <type>char</type>* const* <name>ppEnabledLayerNames</name></member>
      <member optional="true"><type>uint32_t</type> <name>enabledExtensionCount</name></member>
      <member len="enabledExtensionCount,null-terminated">const <type>char</type>* const* <name>ppEnabledExtensionNames</name></member>
    </type>
  </types>
  <enums>
    <enum value="0" name="VK_STRUCTURE_TYPE_APPLICATION_INFO"/>
    <enum value="1" name="VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO"/>
  </enums>
  <commands>
    <command>
      <proto><type>VkResult</type> <name>vkCreateInstance</name></proto>
      <param>const <type>VkInstanceCreateInfo</type>* <name>pCreateInfo</name></param>
      <param optional="true">const <type>VkAllocationCallbacks</type>* <name>pAllocator</name></param>
      <param><type>VkInstance</type>* <name>pInstance</name></param>
    </command>
  </commands>
</registry>`

func TestGenerate(t *testing.T) {
	src, err := Generate([]byte(miniXML), "proof", proofStructs, proofCommands)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	for _, want := range []string{
		"package proof",
		"func EncodeVkInstanceCreateInfo(",
		"func Encode_vkCreateInstance(",
	} {
		if !strings.Contains(string(src), want) {
			t.Errorf("missing %q", want)
		}
	}
}

func TestGenerateParseError(t *testing.T) {
	if _, err := Generate([]byte("<registry><types"), "proof", proofStructs, proofCommands); err == nil {
		t.Error("expected parse error")
	}
}

func TestGenerateEmitError(t *testing.T) {
	if _, err := Generate([]byte(miniXML), "proof", []string{"VkNope"}, nil); err == nil {
		t.Error("expected emit error for unknown struct")
	}
}

func TestRunEndToEnd(t *testing.T) {
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "vk.xml")
	outPath := filepath.Join(dir, "out.go")
	if err := os.WriteFile(xmlPath, []byte(miniXML), 0o644); err != nil {
		t.Fatal(err)
	}

	withArgs(t, []string{"vkgen", "-xml", xmlPath, "-out", outPath}, func() {
		if err := run(); err != nil {
			t.Fatalf("run: %v", err)
		}
	})

	out, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read out: %v", err)
	}
	if !strings.Contains(string(out), "func Encode_vkCreateInstance(") {
		t.Errorf("generated file missing command encoder:\n%s", out)
	}
}

func TestRunReadError(t *testing.T) {
	withArgs(t, []string{"vkgen", "-xml", filepath.Join(t.TempDir(), "missing.xml")}, func() {
		if err := run(); err == nil {
			t.Error("expected read error")
		}
	})
}

func TestRunWriteError(t *testing.T) {
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "vk.xml")
	if err := os.WriteFile(xmlPath, []byte(miniXML), 0o644); err != nil {
		t.Fatal(err)
	}
	// Output path points into a non-existent directory -> write fails.
	bad := filepath.Join(dir, "nope", "out.go")
	withArgs(t, []string{"vkgen", "-xml", xmlPath, "-out", bad}, func() {
		if err := run(); err == nil {
			t.Error("expected write error")
		}
	})
}

func TestRunGenerateError(t *testing.T) {
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "vk.xml")
	if err := os.WriteFile(xmlPath, []byte("<registry><types"), 0o644); err != nil {
		t.Fatal(err)
	}
	withArgs(t, []string{"vkgen", "-xml", xmlPath}, func() {
		if err := run(); err == nil {
			t.Error("expected generate error")
		}
	})
}

func TestGenerateFormatError(t *testing.T) {
	saved := formatSource
	formatSource = func([]byte) ([]byte, error) { return nil, errors.New("boom") }
	defer func() { formatSource = saved }()
	if _, err := Generate([]byte(miniXML), "proof", proofStructs, proofCommands); err == nil {
		t.Error("expected gofmt error")
	}
}

func TestRunFlagError(t *testing.T) {
	// Unknown flag -> fs.Parse returns an error (covers the parse-error
	// branch in run).
	withArgs(t, []string{"vkgen", "-nope"}, func() {
		if err := run(); err == nil {
			t.Error("expected flag parse error")
		}
	})
}

// TestMainExitsOnError re-execs the test binary so we can observe main()
// calling os.Exit(1) on a run error (the only way to cover that branch).
func TestMainExitsOnError(t *testing.T) {
	if os.Getenv("VKGEN_CRASH") == "1" {
		os.Args = []string{"vkgen", "-xml", filepath.Join(t.TempDir(), "missing.xml")}
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestMainExitsOnError")
	cmd.Env = append(os.Environ(), "VKGEN_CRASH=1")
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.Success() {
		t.Fatalf("expected non-zero exit from main(), got %v", err)
	}
}

func TestMain_(t *testing.T) {
	// main() with a successful run (covers the os.Exit-free happy path).
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "vk.xml")
	outPath := filepath.Join(dir, "out.go")
	if err := os.WriteFile(xmlPath, []byte(miniXML), 0o644); err != nil {
		t.Fatal(err)
	}
	withArgs(t, []string{"vkgen", "-xml", xmlPath, "-out", outPath}, func() {
		main()
	})
}
