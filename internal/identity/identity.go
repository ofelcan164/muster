// Package identity derives a repo's look from its name alone.
//
// Nothing here knows any repo name in advance. Everything is a pure function of
// the identity key, so the same repo looks identical on every machine and a
// brand new repo gets a usable look on first sight with no configuration.
package identity

import "hash/fnv"

// Palette is the fixed eight-slot colour set. Hues are snapped to these slots
// rather than spread continuously so two repos can never land close enough to
// be confused at the edge of vision.
var Palette = []string{
	"#83a598", // blue
	"#b8bb26", // green
	"#d3869b", // purple
	"#fe8019", // orange
	"#8ec07c", // aqua
	"#fabd2f", // yellow
	"#fb4934", // red
	"#d5c4a1", // fg2
}

// Sigils survive colourblindness and a bad terminal palette. They are assigned
// round-robin in discovery order rather than hashed, so no two repos can
// collide until there are more repos than sigils. That is the point of having
// the axis at all: hue is hashed and may collide, and the sigil is what still
// tells two same-coloured repos apart.
var Sigils = []string{"✦", "◆", "▣", "⬢", "⬡", "◈", "▲", "●"}

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
// directory and every worktree that has ever existed spends one. Once nine have
// been handed out, the tenth repo wraps onto the first repo's sigil, and two
// cards visible at the same time carried the same mark: exactly the collision
// the sigil exists to rule out. Preferring the slot's own sigil keeps it stable
// while the set of repos on screen is, and only the card that would collide
// moves.
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
