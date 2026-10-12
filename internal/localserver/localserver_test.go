package localserver

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestModelsDirFollowsEachSystem: one default per system, each that
// system's convention, and JITLLM_MODELS over all of them.
func TestModelsDirFollowsEachSystem(t *testing.T) {
	home := filepath.FromSlash("/h")
	for _, c := range []struct {
		goos, data, local, want string
	}{
		{"linux", "", "", "/h/.local/share/jitllm/models"},
		{"linux", "/xdg", "", "/xdg/jitllm/models"},
		{"linux", "relative", "", "/h/.local/share/jitllm/models"},
		{"freebsd", "/xdg", "", "/xdg/jitllm/models"},
		{"darwin", "/xdg", "", "/h/Library/Application Support/jitllm/models"},
		{"windows", "", "/L", "/L/jitllm/models"},
		{"windows", "", "", "/h/AppData/Local/jitllm/models"},
	} {
		got := ModelsDirFor(c.goos, filepath.FromSlash(c.data), home, filepath.FromSlash(c.local))
		if got != filepath.FromSlash(c.want) {
			t.Errorf("%s data=%q local=%q: %q, want %q", c.goos, c.data, c.local, got, c.want)
		}
	}
	if got := ModelsDir("/explicit", "/xdg"); got != "/explicit" {
		t.Errorf("JITLLM_MODELS set: %q", got)
	}
}

// TestSearchDirsFindsTheLegacyFolder: ./models is still searched, after the
// new folder, and only when it exists.
func TestSearchDirsFindsTheLegacyFolder(t *testing.T) {
	t.Chdir(t.TempDir())
	if d := SearchDirs("/new"); len(d) != 1 {
		t.Fatalf("no ./models: %v", d)
	}
	if err := os.Mkdir(LegacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if d := SearchDirs("/new"); len(d) != 2 || d[1] != LegacyDir {
		t.Fatalf("with ./models: %v", d)
	}
	if d := SearchDirs(LegacyDir); len(d) != 1 {
		t.Fatalf("./models as the primary is not listed twice: %v", d)
	}
}

// TestProbeTellsAbsentFromIncompatible.
func TestProbeTellsAbsentFromIncompatible(t *testing.T) {
	serve := func(body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/capabilities" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(body))
		}))
	}
	ok := serve(`{"schema":1,"server_api":1,"build":{"version":"v1"}}`)
	defer ok.Close()
	if in, err := Probe(context.Background(), ok.URL); err != nil || in.Version != "v1" {
		t.Fatalf("a jitllmd: %+v %v", in, err)
	}
	newer := serve(`{"schema":1,"server_api":2}`)
	defer newer.Close()
	if _, err := Probe(context.Background(), newer.URL); !errors.Is(err, ErrIncompatible) {
		t.Fatalf("API 2: %v, want ErrIncompatible", err)
	}
	other := serve(`{"hello":1}`)
	defer other.Close()
	if _, err := Probe(context.Background(), other.URL); err == nil || errors.Is(err, ErrIncompatible) {
		t.Fatalf("not a jitllmd: %v", err)
	}
}
