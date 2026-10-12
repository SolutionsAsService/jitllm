package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConvertOutputDirAppliesToALocalFile holds -o to its meaning for a GGUF
// already on the disk: the container lands in the directory under the
// derived name, not beside the source.
func TestConvertOutputDirAppliesToALocalFile(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "models", "stories260K.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	srcDir, out := t.TempDir(), filepath.Join(t.TempDir(), "made-by-o")
	src := filepath.Join(srcDir, "stories260K.gguf")
	if err := os.WriteFile(src, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JITLLM_MODELS", "")
	if err := convertCmd([]string{"-o", out, src}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "stories260K.jlm")); err != nil {
		t.Errorf("-o %s: no container there: %v", out, err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "stories260K.jlm")); err == nil {
		t.Errorf("-o was given, and the container was written beside the source")
	}
}

// TestConvertWithNoOutputWritesTheModelsFolder: with neither -o nor an output
// path, the container lands in the per-system models folder (created on
// first use) that jitllmd and the apps read, never beside the source.
func TestConvertWithNoOutputWritesTheModelsFolder(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "models", "stories260K.gguf"))
	if err != nil {
		t.Fatal(err)
	}
	srcDir, data := t.TempDir(), t.TempDir()
	src := filepath.Join(srcDir, "stories260K.gguf")
	if err := os.WriteFile(src, b, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("JITLLM_MODELS", "")
	t.Setenv("XDG_DATA_HOME", data)
	t.Setenv("HOME", data) // macOS roots the folder at home
	t.Setenv("LOCALAPPDATA", data)
	want := filepath.Join(defaultModelDir(), "stories260K.jlm")
	if !strings.HasPrefix(want, data) {
		t.Fatalf("the models folder %s is not under the temp home %s", want, data)
	}
	if err := convertCmd([]string{src}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(want); err != nil {
		t.Errorf("no container in the models folder: %v", err)
	}
	if _, err := os.Stat(filepath.Join(srcDir, "stories260K.jlm")); err == nil {
		t.Errorf("the container was written beside the source")
	}
}
