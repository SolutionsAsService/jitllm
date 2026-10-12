package installtest

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestLinuxInstallsTheService: on Linux, installing jitllmd runs `jitllmd
// service install` when a systemd user session answers, and --no-service or
// JITLLM_NO_SERVICE skips it. jitllmd and systemctl are stubs that log.
func TestLinuxInstallsTheService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is the Linux and macOS installer")
	}
	if os.Geteuid() == 0 {
		t.Skip("as root install.sh installs no per-person service")
	}
	for _, c := range []struct {
		name string
		args []string
		env  []string
		want bool
	}{
		{"default", nil, nil, true},
		{"flag", []string{"--no-service"}, nil, false},
		{"env", nil, []string{"JITLLM_NO_SERVICE=1"}, false},
		{"cli only", []string{"jitllm"}, nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			root := t.TempDir()
			log := filepath.Join(root, "calls")
			rel := filepath.Join(root, "rel", "v1.2.3")
			must(t, os.MkdirAll(rel, 0o755))
			var sums strings.Builder
			for _, p := range []string{"jitllm", "jitllmd"} {
				name := p + "_1.2.3_linux_amd64.tar.gz"
				body := "#!/bin/sh\necho " + p + " \"$@\" >> " + log + "\n"
				writeTarGz(t, filepath.Join(rel, name), p, body)
				b, err := os.ReadFile(filepath.Join(rel, name))
				must(t, err)
				s := sha256.Sum256(b)
				sums.WriteString(hex.EncodeToString(s[:]) + "  " + name + "\n")
			}
			must(t, os.WriteFile(filepath.Join(rel, "checksums.txt"), []byte(sums.String()), 0o644))

			stub := filepath.Join(root, "stub")
			must(t, os.MkdirAll(stub, 0o755))
			must(t, os.WriteFile(filepath.Join(stub, "uname"), []byte(
				"#!/bin/sh\ncase $1 in -s) echo Linux ;; -m) echo x86_64 ;; esac\n"), 0o755))
			must(t, os.WriteFile(filepath.Join(stub, "systemctl"), []byte("#!/bin/sh\nexit 0\n"), 0o755))

			cmd := exec.Command("sh", append([]string{"../install.sh"}, c.args...)...)
			cmd.Env = append(os.Environ(),
				"PATH="+stub+":"+os.Getenv("PATH"),
				"HOME="+filepath.Join(root, "home"),
				"JITLLM_VERSION=v1.2.3",
				"JITLLM_DOWNLOAD_URL=file://"+filepath.Join(root, "rel"),
				"JITLLM_INSTALL_DIR="+filepath.Join(root, "bin"),
			)
			cmd.Env = append(cmd.Env, c.env...)
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("install.sh: %v\n%s", err, out)
			}
			calls, _ := os.ReadFile(log)
			got := strings.Contains(string(calls), "jitllmd service install")
			if got != c.want {
				t.Fatalf("service installed: %v, want %v\ncalls:\n%s\noutput:\n%s", got, c.want, calls, out)
			}
		})
	}
}

func writeTarGz(t *testing.T, path, name, body string) {
	t.Helper()
	f, err := os.Create(path)
	must(t, err)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	must(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body))}))
	_, err = tw.Write([]byte(body))
	must(t, err)
	must(t, tw.Close())
	must(t, gz.Close())
	must(t, f.Close())
}
