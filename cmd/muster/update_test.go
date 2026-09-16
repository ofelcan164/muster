package main

import "testing"

func TestNewestTag(t *testing.T) {
	out := "a1\trefs/tags/v0.1.0\n" +
		"b2\trefs/tags/v0.10.0\n" +
		"c3\trefs/tags/v0.9.9\n" +
		"d4\trefs/tags/v1.0.0-rc1\n" +
		"e5\trefs/tags/nightly\n"
	if got := newestTag(out); got != "v0.10.0" {
		t.Errorf("newestTag = %q, want v0.10.0", got)
	}
	if got := newestTag(""); got != "" {
		t.Errorf("newestTag of nothing = %q, want empty", got)
	}
}

func TestNewer(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "0.1.0", true},
		{"v0.1.0", "0.1.0", false},
		{"v0.1.0", "0.2.0", false},
		{"v0.10.0", "0.9.0", true},
		{"v0.1.0", "", true},
	} {
		if got := newer(c.a, c.b); got != c.want {
			t.Errorf("newer(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
