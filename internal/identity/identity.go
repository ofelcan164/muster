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

// Borders are redundant on purpose: a fourth axis costs nothing and any one of
// the four landing is enough to tell two repos apart.
var Borders = []string{"solid", "double", "dashed", "dotted"}

func hash(key string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	return h.Sum32()
}

// Look returns the hashed axes for an identity key: the colour slot and the
// border style. Both are pure functions of the key, so a repo looks the same on
// every machine. The sigil is not hashed; see Sigil.
func Look(key string) (colorIndex int, border string) {
	h := hash(key)
	colorIndex = int(h % uint32(len(Palette)))
	// Draw the border off different bits so a colour collision does not imply a
	// border collision.
	border = Borders[int((h>>16)%uint32(len(Borders)))]
	return
}

// Color returns the hex colour for an index produced by Look.
func Color(i int) string {
	if i < 0 || i >= len(Palette) {
		return "#928374" // neutral grey, used for non-git scratch workspaces
	}
	return Palette[i]
}

// NeutralColor is the card colour for a workspace with no repository.
const NeutralColor = "#928374"

// NeutralSigil marks a scratch workspace.
const NeutralSigil = "○"
