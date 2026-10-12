// Package installtest runs scripts/install.sh against a release laid out on
// disk, with uname answering as the host under test.
package installtest

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestDesktopOnMacInstallsTheApp: on macOS `install.sh desktop` installs the
// release's signed jitllm.app, not the bare binary, into the apps directory,
// or into ~/Applications when that directory is not writable.
func TestDesktopOnMacInstallsTheApp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is the Linux and macOS installer; Windows has install.ps1")
	}
	for _, tool := range []string{"sh", "curl", "unzip"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("%s is needed: %v", tool, err)
		}
	}
	t.Run("writable", func(t *testing.T) {
		root := t.TempDir()
		apps := filepath.Join(root, "apps")
		must(t, os.MkdirAll(apps, 0o755))
		installApp(t, root, apps, apps)
	})
	t.Run("not writable", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root writes a read-only directory, so the fallback cannot be reached")
		}
		root := t.TempDir()
		apps := filepath.Join(root, "apps")
		must(t, os.MkdirAll(apps, 0o555))
		installApp(t, root, apps, filepath.Join(root, "home", "Applications"))
	})
}

// installApp runs install.sh desktop as an arm64 Mac with JITLLM_APPS_DIR=apps
// and requires jitllm.app in want.
func installApp(t *testing.T, root, apps, want string) {
	t.Helper()
	// The release: the .app.zip and a checksums.txt naming it.
	rel := filepath.Join(root, "rel", "v1.2.3")
	must(t, os.MkdirAll(rel, 0o755))
	name := "jitllm_1.2.3_darwin_arm64.app.zip"
	f, err := os.Create(filepath.Join(rel, name))
	must(t, err)
	zw := zip.NewWriter(f)
	w, err := zw.Create("jitllm.app/Contents/MacOS/jitllm-desktop")
	must(t, err)
	_, err = w.Write([]byte("app"))
	must(t, err)
	must(t, zw.Close())
	must(t, f.Close())
	body, err := os.ReadFile(filepath.Join(rel, name))
	must(t, err)
	sum := sha256.Sum256(body)
	must(t, os.WriteFile(filepath.Join(rel, "checksums.txt"),
		[]byte(hex.EncodeToString(sum[:])+"  "+name+"\n"), 0o644))

	// uname answers as an arm64 Mac.
	stub := filepath.Join(root, "stub")
	must(t, os.MkdirAll(stub, 0o755))
	must(t, os.WriteFile(filepath.Join(stub, "uname"), []byte(
		"#!/bin/sh\ncase $1 in -s) echo Darwin ;; -m) echo arm64 ;; esac\n"), 0o755))

	home := filepath.Join(root, "home")
	must(t, os.MkdirAll(home, 0o755))
	cmd := exec.Command("sh", "../install.sh", "desktop")
	cmd.Env = append(os.Environ(),
		"PATH="+stub+":"+os.Getenv("PATH"),
		"HOME="+home,
		"JITLLM_VERSION=v1.2.3",
		"JITLLM_DOWNLOAD_URL=file://"+filepath.Join(root, "rel"),
		"JITLLM_INSTALL_DIR="+filepath.Join(root, "bin"),
		"JITLLM_APPS_DIR="+apps,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install.sh desktop: %v\n%s", err, out)
	}
	app := filepath.Join(want, "jitllm.app")
	got, err := os.ReadFile(filepath.Join(app, "Contents", "MacOS", "jitllm-desktop"))
	if err != nil || string(got) != "app" {
		t.Fatalf("jitllm.app not installed in %s (%v)\n%s", want, err, out)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "jitllm-desktop")); err == nil {
		t.Fatalf("a bare jitllm-desktop was installed beside the app")
	}
	if !strings.Contains(string(out), "installed "+app) {
		t.Fatalf("install.sh did not report the app:\n%s", out)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
