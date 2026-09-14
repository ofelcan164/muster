package identity

import "testing"

// Identity must be a pure function of the key so a repo looks the same on every
// machine you use.
func TestLookDeterministic(t *testing.T) {
	for i := 0; i < 100; i++ {
		if c1, c2 := Look("acme/api"), Look("acme/api"); c1 != c2 {
			t.Fatalf("Look not deterministic: %d vs %d", c1, c2)
		}
	}
}

func TestLookInRange(t *testing.T) {
	for _, key := range []string{"a", "acme/api", "otherorg/web", "dir:/tmp/x", ""} {
		if c := Look(key); c < 0 || c >= len(Palette) {
			t.Errorf("Look(%q) colour %d out of range", key, c)
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

// Slots are pinned for the life of an install and never freed, so a machine
// that has seen twenty-one repos hands the twenty-second a sigil already on
// screen. Two cards carrying the same mark is the one thing the sigil axis exists to prevent.
func TestSigilsDoNotCollideOnScreen(t *testing.T) {
	taken := map[string]bool{}
	seen := map[string]int{}
	for _, slot := range []int{0, 1, 2, 8, 9, 17} {
		s := SigilFor(slot, taken)
		if taken[s] {
			t.Fatalf("slot %d reused sigil %q", slot, s)
		}
		taken[s] = true
		seen[s] = slot
	}
	// The lowest slots keep the sigil they have always had, so a repo's mark
	// does not move because another one appeared.
	for slot := range 3 {
		if got := SigilFor(slot, map[string]bool{}); got != Sigils[slot] {
			t.Errorf("slot %d took %q rather than its own %q", slot, got, Sigils[slot])
		}
	}
}

// More repos on screen than there are sigils is the one case with no answer.
// Repeating is better than drawing nothing.
func TestSigilFallsBackWhenEveryMarkIsTaken(t *testing.T) {
	taken := map[string]bool{}
	for _, s := range Sigils {
		taken[s] = true
	}
	if got := SigilFor(3, taken); got != Sigils[3] {
		t.Errorf("SigilFor with everything taken = %q, want its own %q", got, Sigils[3])
	}
}
