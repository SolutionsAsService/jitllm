package mock

import (
	"strings"
	"testing"

	"github.com/jitllm/jitllm/ui/app"
	"github.com/jitllm/jitllm/ui/screen"
)

// A reply keeps being written into the chat that asked for it while another
// chat is on screen: the other chat stays untouched, and the asking chat has
// the whole reply when it is shown again.
func TestAReplyStreamsIntoItsOwnChat(t *testing.T) {
	sh := app.NewShell(nil, nil)
	deps, e := Deps(sh)
	screen.Attach(sh, deps)
	e.Load(Llama().Entry.Path)
	sh.DrainQueue(0)
	st := sh.Store
	if !st.Loaded.Get() {
		t.Fatal("the fixture did not load")
	}

	st.Draft.Set("What is the capital of France?")
	screen.Submit(sh)
	for range 3 {
		e.Step()
	}
	sh.DrainQueue(0)
	if !st.Streaming.Get() {
		t.Fatal("no reply is streaming: this gate is testing nothing")
	}

	st.NewChat() // chat B, first in the list; the asking chat is now 1
	for e.Step() {
		sh.DrainQueue(0)
		if n := st.Turns.Get(); n != 0 {
			t.Fatalf("the other chat shows %d turn(s) while the reply streams", n)
		}
	}
	sh.DrainQueue(0)
	if b, _ := st.ChatAt(0); len(b.Turns) != 0 {
		t.Fatalf("the other chat holds %d turn(s), want none", len(b.Turns))
	}

	st.SelectChat(1)
	want := strings.Join(strings.Fields(Llama().Reply), " ")
	if st.Turns.Get() != 2 || st.Turn(1).Text != want {
		t.Fatalf("the asking chat came back with %d turn(s), reply %q", st.Turns.Get(), st.Turn(1).Text)
	}
}
