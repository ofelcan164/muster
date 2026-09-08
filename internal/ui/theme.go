// Package ui is the overlay: the screen one key opens.
//
// It does no work beyond reading the snapshot the daemon already wrote. That is
// the whole reason the daemon exists, and it is what keeps opening this screen
// a hundred times a day from costing anything.
package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/ofelcan/muster/internal/identity"
	"github.com/ofelcan/muster/internal/model"
)

// Gruvbox, matching the design. Read from the herdr theme later; hardcoded now
// so the overlay looks right before any config exists.
var (
	colBG     = lipgloss.Color("#1d2021")
	colFG     = lipgloss.Color("#ebdbb2")
	colDim    = lipgloss.Color("#928374")
	colFaint  = lipgloss.Color("#665c54")
	colRed    = lipgloss.Color("#fb4934")
	colGreen  = lipgloss.Color("#b8bb26")
	colYellow = lipgloss.Color("#fabd2f")
	colBlue   = lipgloss.Color("#83a598")
	colOrange = lipgloss.Color("#fe8019")
	colSelBG  = lipgloss.Color("#3c3836")
	colPanel  = lipgloss.Color("#282828")
	colHotBG  = lipgloss.Color("#3a2a26")
)

var (
	styTitle   = lipgloss.NewStyle().Foreground(colYellow).Bold(true)
	styMeta    = lipgloss.NewStyle().Foreground(colDim)
	stySection = lipgloss.NewStyle().Foreground(colDim)
	styDim     = lipgloss.NewStyle().Foreground(colDim)
	styFaint   = lipgloss.NewStyle().Foreground(colFaint)
	styFG      = lipgloss.NewStyle().Foreground(colFG)
	styWarn    = lipgloss.NewStyle().Foreground(colOrange)
	styHint    = lipgloss.NewStyle().Foreground(colFaint)
	stySel     = lipgloss.NewStyle().Background(colSelBG)
	styPanel   = lipgloss.NewStyle().Background(colPanel)
	styHot     = lipgloss.NewStyle().Background(colHotBG)
	styMatch   = lipgloss.NewStyle().Foreground(colYellow).Bold(true)
)

// statusStyle colours an agent by what it needs from you, not by what it is.
func statusStyle(s model.Status) lipgloss.Style {
	switch s {
	case model.StatusBlocked:
		return lipgloss.NewStyle().Foreground(colRed)
	case model.StatusDone:
		return lipgloss.NewStyle().Foreground(colGreen)
	case model.StatusWorking:
		return lipgloss.NewStyle().Foreground(colYellow)
	case model.StatusIdle:
		return lipgloss.NewStyle().Foreground(colDim)
	default:
		return lipgloss.NewStyle().Foreground(colFaint)
	}
}

func statusIcon(s model.Status) string {
	switch s {
	case model.StatusBlocked:
		return "▲"
	case model.StatusDone:
		return "●"
	case model.StatusWorking:
		return "◐"
	case model.StatusIdle:
		return "○"
	default:
		return "◌"
	}
}

// reasonAccent is the colour a ribbon row is keyed to. It comes from why the
// row is there rather than from the agent's status, because the ribbon answers
// "why does this need me" and the grid already answers "what state is it in".
func reasonAccent(r model.Reason) lipgloss.Color {
	switch r {
	case model.ReasonBlocked:
		return colRed
	case model.ReasonGateUntold:
		return colOrange
	case model.ReasonProcessStopped:
		return colYellow
	case model.ReasonDoneUnseen:
		return colGreen
	default:
		return colDim
	}
}

// reasonLabel is the short word in the badge. It says why, not what: an agent
// sitting at a permission prompt is "BLOCKED", but one whose dev server died is
// "STOPPED", and those read differently at a glance.
func reasonLabel(r model.Reason, status model.Status) string {
	switch r {
	case model.ReasonGateUntold:
		return "GATE"
	case model.ReasonProcessStopped:
		return "STOPPED"
	case model.ReasonIdleNeverDone:
		return "STALE"
	case model.ReasonBlocked:
		return "BLOCKED"
	case model.ReasonDoneUnseen:
		return "DONE"
	default:
		return strings.ToUpper(string(status))
	}
}

// badge is a filled label. Solid colour on the accent reads at the edge of
// vision, which is the whole job of the ribbon.
func badge(text string, accent lipgloss.Color) string {
	return lipgloss.NewStyle().
		Background(accent).Foreground(colBG).Bold(true).
		Render(" " + text + " ")
}

// accentBar is the left edge marking a row's urgency without spending width.
func accentBar(accent lipgloss.Color) string {
	return lipgloss.NewStyle().Foreground(accent).Render("▌")
}

// repoStyle is the repo's hashed colour. A scratch workspace with no repository
// gets neutral grey, which is how it reads as "not one of your repos" without
// anything having to say so.
func repoStyle(r model.Repo) lipgloss.Style {
	if r.ColorIndex < 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(identity.NeutralColor))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(identity.Color(r.ColorIndex)))
}

// paint lays a background across a line that already carries its own colours.
// lipgloss ends every inner style with a full reset, so wrapping styled text in
// a background style loses that background from the first inner reset onward:
// the card would light up in stripes. Re-arming the background after each reset
// is what makes a hovered card highlight as one block.
func paint(line string, style lipgloss.Style) string {
	probe := style.Render("x")
	i := strings.Index(probe, "x")
	if i <= 0 {
		return line // no colour profile, nothing to re-arm
	}
	seq := probe[:i]
	return seq + strings.ReplaceAll(line, reset, reset+seq) + reset
}

const reset = "\x1b[0m"
