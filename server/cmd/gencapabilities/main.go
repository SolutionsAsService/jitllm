// Command gencapabilities writes the capability manifest: the server's half,
// internal/capabilities/api.json, from server.API (its route table and
// request structs), and then docs/capabilities.json, the whole manifest of the
// source tree. It runs in this module because only this module sees both
// halves:
//
//	cd server && go run ./cmd/gencapabilities
//
// internal/srcgate.TestCapabilitiesDocIsGenerated and
// server.TestAPIManifestIsGenerated fail until it has run.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jitllm/jitllm/internal/capabilities"
	"github.com/jitllm/jitllm/server"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "gencapabilities:", err)
		os.Exit(1)
	}
}

func run() error {
	root, err := filepath.Abs("..")
	if err != nil {
		return err
	}
	if _, err := os.Stat(filepath.Join(root, "AGENTS.md")); err != nil {
		return fmt.Errorf("run it from server/: %s is not the repository root", root)
	}
	a, err := capabilities.RenderAPI(server.API())
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(capabilities.APIPath)), a, 0o644); err != nil {
		return err
	}
	// The document embeds what was just written, so it is rendered from the
	// same API value rather than from the stale embedded copy.
	m, err := capabilities.DocWith(server.API())
	if err != nil {
		return err
	}
	b, err := capabilities.Render(m)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(root, filepath.FromSlash(capabilities.Path)), b, 0o644)
}
