package localserver

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFindByNameStemAndAlias: Find answers a file name, its stem and an
// alias, in folder order, and AddAlias refuses an alias another container
// answers to.
func TestFindByNameStemAndAlias(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	for _, p := range []string{filepath.Join(a, "m.jlm"), filepath.Join(b, "m.jlm"), filepath.Join(b, "n.jlm")} {
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"m", "m.jlm"} {
		if p, _, err := Find([]string{a, b}, name); err != nil || p != filepath.Join(a, "m.jlm") {
			t.Errorf("Find(%q) = %q, %v; the first folder wins", name, p, err)
		}
	}
	if ok, err := AddAlias(b, "n.jlm", "m"); ok || err != nil {
		t.Errorf("AddAlias of another file's stem: %v, %v", ok, err)
	}
	if ok, err := AddAlias(b, "n.jlm", "qwen"); !ok || err != nil {
		t.Fatalf("AddAlias: %v, %v", ok, err)
	}
	if p, _, _ := Find([]string{a, b}, "qwen"); p != filepath.Join(b, "n.jlm") {
		t.Errorf("Find by alias: %q", p)
	}
	if p, _, _ := Find([]string{a, b}, "nope"); p != "" {
		t.Errorf("Find of nothing: %q", p)
	}
}
