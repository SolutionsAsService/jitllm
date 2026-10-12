package engine

import "testing"

func TestBadgeSaysWhichEngine(t *testing.T) {
	for _, c := range []struct {
		w    Where
		api  string
		want string
	}{
		{Where{}, "", "Embedded · API off"},
		{Where{}, "127.0.0.1:8080", "Embedded · API on :8080"},
		{Where{}, "0.0.0.0:9000", "Embedded · API on 0.0.0.0:9000"},
		{Where{Service: true, Addr: "127.0.0.1:8080", Version: "0.9.0"}, "", "Service · 127.0.0.1:8080 (jitllmd 0.9.0)"},
		{Where{Service: true, Addr: "127.0.0.1:8080", Version: "0.9.0", Lost: true}, "", "Service · 127.0.0.1:8080 (jitllmd 0.9.0) · not answering"},
	} {
		if got := Badge(c.w, c.api); got != c.want {
			t.Errorf("Badge(%+v, %q) = %q, want %q", c.w, c.api, got, c.want)
		}
	}
}
