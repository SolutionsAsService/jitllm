package main

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/jitllm/jitllm/common/api"
	"github.com/jitllm/jitllm/common/engine"
	"github.com/jitllm/jitllm/server"
)

// footerOf is the status bar's text.
func footerOf(r *root) string {
	lines := strings.Split(stripANSI(r.View()), "\n")
	return lines[len(lines)-1]
}

// The status bar says which engine runs on every screen and at every width,
// and what the API is doing.
func TestTheStatusBarAlwaysSaysWhichEngine(t *testing.T) {
	r := newTestRoot(t)
	for _, w := range []int{160, 80, 60} {
		r.Update(tea.WindowSizeMsg{Width: w, Height: 30})
		for s := range screenNames {
			r.setScreen(screen(s))
			if f := footerOf(r); !strings.Contains(f, "Embedded · API off") {
				t.Errorf("%s at %d columns: no badge in %q", screenNames[s], w, f)
			}
		}
	}
	r.Update(tea.WindowSizeMsg{Width: 160, Height: 44})
	r.env.cfg.API, r.env.cfg.APIAddr = true, "127.0.0.1:0"
	r.env.syncAPI()
	defer r.env.api.Close()
	if f := footerOf(r); !strings.Contains(f, "Embedded · API on :") {
		t.Errorf("API on, the badge says %q", f)
	}
	r.env.where = engine.Where{Service: true, Addr: "127.0.0.1:8080", Version: "1.2.3"}
	if f := footerOf(r); !strings.Contains(f, "Service · 127.0.0.1:8080 (jitllmd 1.2.3)") {
		t.Errorf("connected, the badge says %q", f)
	}
}

// Turning the API on where a jitllmd answers is refused, the setting goes
// back off, and the line says how to connect instead.
func TestTheAPIIsRefusedWhereAServiceAnswers(t *testing.T) {
	svc := server.New(server.Config{ModelDir: t.TempDir(), Version: "4.5.6"})
	defer svc.Close()
	held, err := api.Start("127.0.0.1:0", svc)
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()

	r := newTestRoot(t)
	r.env.cfg.API, r.env.cfg.APIAddr = true, held.Addr()
	msg := r.env.syncAPI()
	if r.env.api != nil {
		r.env.api.Close()
		t.Fatalf("an API started beside the service: %q", msg)
	}
	if r.env.cfg.API {
		t.Error("the setting still says the API is on")
	}
	if !strings.Contains(msg, "--connect "+held.Addr()) {
		t.Errorf("the refusal does not say how to connect: %q", msg)
	}
	if f := footerOf(r); !strings.Contains(f, "Embedded · API off") {
		t.Errorf("after the refusal the badge says %q", f)
	}
}
