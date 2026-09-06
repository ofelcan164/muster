package identity

import "testing"

// Identity must be a pure function of the key so a repo looks the same on every
// machine you use.
func TestLookDeterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		c1, b1 := Look("acme/api")
		c2, b2 := Look("acme/api")
		if c1 != c2 || b1 != b2 {
			t.Fatalf("Look not deterministic: (%d,%s) vs (%d,%s)", c1, b1, c2, b2)
		}
	}
}

func TestLookInRange(t *testing.T) {
	for _, key := range []string{"a", "acme/api", "otherorg/web", "dir:/tmp/x", ""} {
		c, b := Look(key)
		if c < 0 || c >= len(Palette) {
			t.Errorf("Look(%q) colour %d out of range", key, c)
		}
		if b == "" {
			t.Errorf("Look(%q) empty border", key)
		}
	}
}

// Sigils are round-robin, not hashed, so no two repos collide until there are
// more repos than sigils.
func TestSigilNoCollisionWithinSet(t *testing.T) {
	seen := map[string]int{}
	for slot := 0; slot < len(Sigils); slot++ {
		s := Sigil(slot)
		if prev, dup := seen[s]; dup {
			t.Fatalf("sigil %q reused by slots %d and %d", s, prev, slot)
		}
		seen[s] = slot
	}
	if Sigil(len(Sigils)) != Sigil(0) {
		t.Error("expected sigils to cycle past the end of the set")
	}
}
