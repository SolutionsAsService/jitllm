// Package localserver is the one place every front end (the CLI, the window,
// the terminal app and jitllmd itself) learns where the local service is and
// where its models live: the default address, the per-system models folder,
// and the check "does a jitllmd answer here, and which version".
//
// It reads no environment: each command reads JITLLM_MODELS, XDG_DATA_HOME
// and its own address flag and hands them in.
package localserver

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultAddr is where the service listens unless told otherwise: loopback
// only, so installing it opens nothing to the network.
const DefaultAddr = "127.0.0.1:8080"

// ModelsDir is the models folder: models ($JITLLM_MODELS) when set, else the
// per-system default (ModelsDirFor with this host's directories).
func ModelsDir(models, dataHome string) string {
	if models != "" {
		return models
	}
	home, _ := os.UserHomeDir()
	local := ""
	if runtime.GOOS == "windows" {
		local, _ = os.UserCacheDir() // %LOCALAPPDATA%
	}
	return ModelsDirFor(runtime.GOOS, dataHome, home, local)
}

// ModelsDirFor is the default models folder on goos: XDG's data home on Linux
// and the other Unixes, Application Support on macOS, the local AppData on
// Windows (where the installer creates it), each with jitllm/models under it.
func ModelsDirFor(goos, dataHome, home, localAppData string) string {
	switch goos {
	case "darwin":
		return filepath.Join(home, "Library", "Application Support", "jitllm", "models")
	case "windows":
		if localAppData == "" {
			localAppData = filepath.Join(home, "AppData", "Local")
		}
		return filepath.Join(localAppData, "jitllm", "models")
	}
	if dataHome == "" || !filepath.IsAbs(dataHome) {
		// The XDG spec: a relative XDG_DATA_HOME is invalid and ignored.
		dataHome = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dataHome, "jitllm", "models")
}

// LegacyDir is where releases before the per-system folder looked: ./models
// in the working directory. It is searched, never written, so nothing moves
// silently.
const LegacyDir = "models"

// SearchDirs is primary, then the legacy folder when it exists and is not the
// same directory.
func SearchDirs(primary string) []string {
	out := []string{primary}
	if fi, err := os.Stat(LegacyDir); err == nil && fi.IsDir() {
		a, _ := filepath.Abs(LegacyDir)
		b, _ := filepath.Abs(primary)
		if a != b {
			out = append(out, LegacyDir)
		}
	}
	return out
}

// Info is what a probe learned about a running service.
type Info struct {
	Addr    string
	Version string
}

// BaseURL turns an address (host:port, :port, or a URL) into the URL a client
// dials.
func BaseURL(addr string) string {
	if strings.HasPrefix(addr, "http://") || strings.HasPrefix(addr, "https://") {
		return strings.TrimRight(addr, "/")
	}
	if strings.HasPrefix(addr, ":") {
		addr = "127.0.0.1" + addr
	}
	return "http://" + addr
}

// Probe asks addr whether a jitllmd answers there, through its capability
// manifest (GET /v1/capabilities, which carries the build's version). A
// refused connection, a timeout, or anything that is not that manifest is an
// error, and the caller runs embedded.
func Probe(ctx context.Context, addr string) (Info, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, BaseURL(addr)+"/v1/capabilities", nil)
	if err != nil {
		return Info{}, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Info{}, fmt.Errorf("%s: /v1/capabilities answered %s, not a jitllmd", addr, resp.Status)
	}
	var m struct {
		Schema int `json:"schema"`
		Build  *struct {
			Version string `json:"version"`
		} `json:"build"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil || m.Schema == 0 {
		return Info{}, fmt.Errorf("%s: /v1/capabilities is not a jitllm manifest", addr)
	}
	in := Info{Addr: addr}
	if m.Build != nil {
		in.Version = m.Build.Version
	}
	return in, nil
}
