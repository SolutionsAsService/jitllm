package session

import "testing"

// Every loaded model reads Loaded, whichever the chat talks to.
func TestEveryLoadedModelIsLoaded(t *testing.T) {
	ms := []LoadedModel{{Path: "a"}, {Path: "b", Active: true}}
	for _, p := range []string{"a", "b"} {
		if got := LoadState(ms, p); got != Loaded {
			t.Errorf("%s reads %q", p, got)
		}
	}
	if got := LoadState(ms, "c"); got != "" {
		t.Errorf("a model not loaded reads %q", got)
	}
}
