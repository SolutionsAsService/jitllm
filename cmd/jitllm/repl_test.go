package main

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// TestReadTurnJoinsContinuedLines: a trailing backslash and a """ block are
// one message, and end of input is io.EOF.
func TestReadTurnJoinsContinuedLines(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("one\\\ntwo\n\"\"\"\nthree\nfour\n\"\"\"\n\"\"\"inline\"\"\"\nlast"))
	for _, want := range []string{"one\ntwo", "three\nfour\n", "inline", "last"} {
		got, err := readTurn(r, io.Discard)
		if err != nil || got != want {
			t.Fatalf("readTurn = %q, %v; want %q", got, err, want)
		}
	}
	if _, err := readTurn(r, io.Discard); err != io.EOF {
		t.Fatalf("at the end: %v, want io.EOF", err)
	}
}

// TestRunReadsAPipedPrompt: with no prompt on the command line and stdin a
// pipe, the pipe is the prompt, completed as a command-line one is.
func TestRunReadsAPipedPrompt(t *testing.T) {
	path := modelPath(t, "stories260K.jlm")
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), cliArgsEnv+"="+strings.Join([]string{"run", "-devices", "cpu", "-n", "4", path}, "\x1f"))
	cmd.Stdin = strings.NewReader("Once upon a time\n")
	var o, e bytes.Buffer
	cmd.Stdout, cmd.Stderr = &o, &e
	if err := cmd.Run(); err != nil {
		t.Fatalf("run: %v\n%s", err, firstLines(e.String(), 6))
	}
	if !strings.HasPrefix(o.String(), "Once upon a time") || len(o.String()) <= len("Once upon a time\n") {
		t.Fatalf("stdout %q: want the piped prompt and its completion", o.String())
	}
}
