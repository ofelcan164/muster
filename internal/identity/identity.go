// Package identity derives a repo's look from its name alone.
//
// Nothing here knows any repo name in advance. Everything is a pure function of
// the identity key, so the same repo looks identical on every machine and a
// brand new repo gets a usable look on first sight with no configuration.
package identity

import "hash/fnv"

// Palette is the fixed twenty-slot colour set. Hues are snapped to these slots
// rather than spread continuously so two repos can never land close enough to
// be confused at the edge of vision. Every entry is picked to hold up on the
// overlay's dark background rather than to match it.
var Palette = []string{
	"#fb4934", // red
	"#ff6b6b", // coral
	"#fe8019", // orange
	"#fab387", // peach
	"#fabd2f", // yellow
	"#f9e2af", // cream
	"#b8bb26", // olive
	"#a6e3a1", // mint
	"#8ec07c", // aqua
	"#94e2d5", // teal
	"#89dceb", // sky
	"#74c7ec", // sapphire
	"#89b4fa", // blue
	"#b4befe", // lavender
	"#cba6f7", // mauve
	"#d3869b", // plum
	"#f5c2e7", // pink
	"#ff79c6", // magenta
	"#83a598", // slate
	"#d5c4a1", // sand
}

// Sigils survive colourblindness and a bad terminal palette. They are assigned
// round-robin in discovery order rather than hashed, so no two repos can
// collide until there are more repos than sigils. That is the point of having
// the axis at all: hue is hashed and may collide, and the sigil is what still
// tells two same-coloured repos apart.
//
// None of these is a status icon. The overlay's working spinner, blocked
// triangle, done dot and idle ring own those shapes, so a repo mark reusing
// one read as a state at a glance.
var Sigils = []string{
	"✦", "◆", "▣", "⬢", "⬡", "◈",
	"◇", "■", "□", "▤", "▥", "▦",
	"⬣", "✧", "★", "☆", "◎", "◉",
	"✚", "╳",
}

// Sigil returns the sigil for a grid slot, cycling once slots exceed the set.
func Sigil(slot int) string {
	if slot < 0 {
		return NeutralSigil
	}
	return Sigils[slot%len(Sigils)]
}

// SigilFor is Sigil, skipping past anything already on screen.
//
// Slots are pinned for the life of an install and never freed, so every scratch
// directory and every worktree that has ever existed spends one. Once twenty-one
// have been handed out, the twenty-second repo wraps onto the first repo's sigil,
// and two cards visible at the same time carried the same mark: exactly the
// collision the sigil exists to rule out. Preferring the slot's own sigil keeps
// it stable while the set of repos on screen is, and only the card that would
// collide moves.
func SigilFor(slot int, taken map[string]bool) string {
	if slot < 0 {
		return NeutralSigil
	}
	for i := range Sigils {
		s := Sigils[(slot+i)%len(Sigils)]
		if !taken[s] {
			return s
		}
	}
	return Sigils[slot%len(Sigils)]
}

func hash(key string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32()
}

// Look returns the colour slot for an identity key. It is a pure function of
// the key, so a repo is the same colour on every machine. The sigil is not
// hashed; see Sigil.
func Look(key string) int {
	return int(hash(key) % uint32(len(Palette)))
}

// Color returns the hex colour for an index produced by Look.
func Color(i int) string {
	if i < 0 || i >= len(Palette) {
		return NeutralColor
	}
	return Palette[i]
}

// NeutralColor is the card colour for a workspace with no repository.
const NeutralColor = "#928374"

// NeutralSigil marks a scratch workspace.
const NeutralSigil = "○"
