package tok

import "testing"

func TestCompleteHoldsBackOnlyAnUnfinishedRune(t *testing.T) {
	for _, c := range []struct {
		s    string
		want int
	}{
		{"", 0}, {"abc", 3}, {"a€", 4},
		{"a\xe2", 1}, {"a\xe2\x82", 1}, {"a\xf0\x9f\x98", 1},
		{"a\x80b", 3}, {"a\x80", 2}, {"a\xff", 2}, {"\xe2\x82\xac\x80", 4},
	} {
		if got := Complete(c.s); got != c.want {
			t.Errorf("Complete(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}
