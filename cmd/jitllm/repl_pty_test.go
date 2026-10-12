//go:build linux

package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// openPTY returns a pseudo-terminal's controlling and terminal ends.
func openPTY(t *testing.T) (ctl, term *os.File) {
	t.Helper()
	ctl, err := os.OpenFile("/dev/ptmx", os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	var unlock int32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, ctl.Fd(), syscall.TIOCSPTLCK, uintptr(unsafe.Pointer(&unlock))); e != 0 {
		t.Fatal(e)
	}
	var n uint32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, ctl.Fd(), syscall.TIOCGPTN, uintptr(unsafe.Pointer(&n))); e != 0 {
		t.Fatal(e)
	}
	term, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", n), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		t.Fatal(err)
	}
	return ctl, term
}

// lockedBuf is the child's stdout, read while it is written.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// TestRunWithoutAPromptOnATerminalChats: `jitllm run model.jlm` on a terminal
// holds a conversation. The second turn sees the first; Ctrl+C stops a reply
// and not the process; /help lists the commands; /bye exits cleanly.
func TestRunWithoutAPromptOnATerminalChats(t *testing.T) {
	path := modelPath(t, "Qwen3-0.6B-Q8_0.jlm")
	ctl, term := openPTY(t)
	defer ctl.Close()
	// Drain what the terminal echoes, so it never fills.
	go func() { io.Copy(io.Discard, ctl) }()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), cliArgsEnv+"="+strings.Join([]string{"run", "-devices", "cpu", "-n", "400", path}, "\x1f"))
	var out lockedBuf
	var errb bytes.Buffer
	cmd.Stdin, cmd.Stdout, cmd.Stderr = term, &out, &errb
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	term.Close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cmd.Process.Kill()

	// waitFor waits until stdout holds want count times.
	waitFor := func(want string, count int) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Minute)
		for strings.Count(out.String(), want) < count {
			select {
			case err := <-done:
				t.Fatalf("exited (%v) waiting for %q; stdout:\n%s\nstderr:\n%s", err, want, out.String(), firstLines(errb.String(), 8))
			case <-time.After(50 * time.Millisecond):
			}
			if time.Now().After(deadline) {
				t.Fatalf("no %q; stdout:\n%s", want, out.String())
			}
		}
	}
	send := func(s string) {
		t.Helper()
		if _, err := ctl.WriteString(s); err != nil {
			t.Fatal(err)
		}
	}

	waitFor(">>> ", 1)
	send("/help\n")
	waitFor("/bye", 1)
	waitFor(">>> ", 2)
	send("My name is Zorblat. Remember it.\\\nReply in one short sentence. /no_think\n")
	waitFor(">>> ", 3)
	send("\"\"\"\nWhat is my name?\nAnswer with the name only. /no_think\n\"\"\"\n")
	waitFor(">>> ", 4)
	second := out.String()
	second = second[strings.LastIndex(second[:len(second)-4], ">>> "):]
	if !strings.Contains(second, "Zorblat") {
		t.Fatalf("the second turn did not see the first; it said:\n%s", second)
	}
	send("Write a very long story about the sea.\n")
	time.Sleep(2 * time.Second)
	waitFor(">>> ", 4)
	before := len(out.String())
	for len(out.String()) == before {
		time.Sleep(50 * time.Millisecond)
	}
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	waitFor("[stopped]", 1)
	waitFor(">>> ", 5)
	send("/bye\n")
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("/bye: %v\n%s", err, firstLines(errb.String(), 8))
		}
	case <-time.After(time.Minute):
		t.Fatal("/bye did not exit")
	}
}
