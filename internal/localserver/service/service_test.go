package service

import (
	"encoding/xml"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type recorder struct{ cmds []string }

func (r *recorder) run(name string, args ...string) ([]byte, error) {
	r.cmds = append(r.cmds, name+" "+strings.Join(args, " "))
	return nil, nil
}

// TestUnitRunsTheServerOnLoopback: the Linux unit runs jitllmd serve on the
// default loopback address over the models folder, quoted for systemd, is
// restarted only after a failure, and starts with the person's session.
func TestUnitRunsTheServerOnLoopback(t *testing.T) {
	s := Service{GOOS: "linux", Home: "/home/p"}
	u := s.Render(Args("/opt/jitllm bin/jitllmd", "/home/p/50%/models", ""))
	for _, want := range []string{
		`ExecStart="/opt/jitllm bin/jitllmd" "serve" "-addr" "127.0.0.1:8080" "-models" "/home/p/50%%/models"`,
		"Restart=on-failure",
		"WantedBy=default.target",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("the unit lacks %q:\n%s", want, u)
		}
	}
	if got := s.File(); got != "/home/p/.config/systemd/user/jitllm.service" {
		t.Errorf("unit at %s", got)
	}
	if got := (Service{GOOS: "linux", Home: "/home/p", ConfigHome: "/cfg"}).File(); got != "/cfg/systemd/user/jitllm.service" {
		t.Errorf("with XDG_CONFIG_HOME: %s", got)
	}
}

// TestPlistIsTheAgentTheAppInstalls: the macOS agent keeps the label, the
// arguments and the log of the app's switch, and decodes as a plist.
func TestPlistIsTheAgentTheAppInstalls(t *testing.T) {
	s := Service{GOOS: "darwin", Home: "/Users/p&q"}
	p := s.Render(Args("/Applications/jitllm.app/Contents/Resources/bin/jitllmd", "/m<x>", ""))
	var doc struct {
		Strings []string `xml:"dict>array>string"`
	}
	if err := xml.Unmarshal([]byte(p), &doc); err != nil {
		t.Fatalf("not XML: %v\n%s", err, p)
	}
	want := []string{"/Applications/jitllm.app/Contents/Resources/bin/jitllmd", "serve", "-addr", "127.0.0.1:8080", "-models", "/m<x>"}
	if strings.Join(doc.Strings, "|") != strings.Join(want, "|") {
		t.Errorf("arguments %q, want %q", doc.Strings, want)
	}
	if s.File() != "/Users/p&q/Library/LaunchAgents/org.jitllm.service.plist" {
		t.Errorf("plist at %s", s.File())
	}
	if !strings.Contains(p, "<string>org.jitllm.service</string>") || !strings.Contains(p, "Library/Logs/jitllm/jitllmd.log") {
		t.Errorf("label or log missing:\n%s", p)
	}
}

// TestWindowsScriptStartsHidden: the login script runs jitllmd hidden
// (window style 0) with every argument quoted and its output in the log.
func TestWindowsScriptStartsHidden(t *testing.T) {
	s := Service{GOOS: "windows", Home: `C:\Users\p`, LocalAppData: `C:\Users\p\AppData\Local`}
	v := s.Render(Args(`C:\Program Files\jitllm\jitllmd.exe`, `C:\Users\p\AppData\Local\jitllm\models`, ""))
	want := `CreateObject("WScript.Shell").Run "cmd /c """"C:\Program Files\jitllm\jitllmd.exe"" ""serve"" ""-addr"" ""127.0.0.1:8080"" ""-models"" ""C:\Users\p\AppData\Local\jitllm\models"" >> ""C:\Users\p\AppData\Local\jitllm\jitllmd.log"" 2>&1""", 0, False` + "\r\n"
	if filepath.Separator == '\\' && v != want {
		t.Errorf("script\n%s\nwant\n%s", v, want)
	}
	if !strings.Contains(v, `", 0, False`) || !strings.Contains(v, `""serve""`) {
		t.Errorf("script %s", v)
	}
}

// TestInstallIntoATemporaryHome installs, starts, stops and removes the
// service on each system in a temporary home, with the system commands
// recorded rather than run.
func TestInstallIntoATemporaryHome(t *testing.T) {
	for _, c := range []struct {
		goos  string
		cmds  []string
		after []string
	}{
		{"linux", []string{"systemctl --user daemon-reload", "systemctl --user enable jitllm.service", "systemctl --user restart jitllm.service"},
			[]string{"systemctl --user disable --now jitllm.service", "systemctl --user daemon-reload"}},
		{"darwin", []string{"launchctl bootstrap gui/501 "},
			[]string{"launchctl bootout gui/501/org.jitllm.service"}},
		{"windows", []string{`reg add HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v jitllmd /t REG_SZ /d wscript.exe "`, "wscript.exe "},
			[]string{`reg delete HKCU\Software\Microsoft\Windows\CurrentVersion\Run /v jitllmd /f`, "taskkill /F /IM jitllmd.exe"}},
	} {
		t.Run(c.goos, func(t *testing.T) {
			home := t.TempDir()
			r := &recorder{}
			s := Service{GOOS: c.goos, Home: home, UID: 501, Run: r.run}
			if s.Installed() {
				t.Fatal("installed before Install")
			}
			if err := s.Start(); err != ErrNotInstalled {
				t.Fatalf("Start before Install: %v", err)
			}
			if err := s.Install(Args("/bin/jitllmd", filepath.Join(home, "models"), "")); err != nil {
				t.Fatal(err)
			}
			if !s.Installed() || !strings.HasPrefix(s.File(), home) {
				t.Fatalf("not installed at %s", s.File())
			}
			b, err := os.ReadFile(s.File())
			if err != nil || !strings.Contains(string(b), "127.0.0.1:8080") {
				t.Fatalf("file %s: %v\n%s", s.File(), err, b)
			}
			checkPrefixes(t, "install", r.cmds, c.cmds)
			r.cmds = nil
			if err := s.Uninstall(); err != nil {
				t.Fatal(err)
			}
			if s.Installed() {
				t.Fatal("still installed after Uninstall")
			}
			checkPrefixes(t, "uninstall", r.cmds, c.after)
		})
	}
}

func checkPrefixes(t *testing.T, what string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s ran %q, want %d commands starting %q", what, got, len(want), want)
	}
	for i := range want {
		if !strings.HasPrefix(got[i], want[i]) {
			t.Errorf("%s command %d: %q, want it to start %q", what, i, got[i], want[i])
		}
	}
}
