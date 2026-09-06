// Package ui is the overlay: the screen one key opens.
//
// It does no work beyond reading the snapshot the daemon already wrote. That is
// the whole reason the daemon exists, and it is what keeps opening this screen
// a hundred times a day from costing anything.
package ui

import (
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

// repoStyle is the repo's hashed colour. A scratch workspace with no repository
// gets neutral grey, which is how it reads as "not one of your repos" without
// anything having to say so.
func repoStyle(r model.Repo) lipgloss.Style {
	if r.ColorIndex < 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(identity.NeutralColor))
	}
	return lipgloss.NewStyle().Foreground(lipgloss.Color(identity.Color(r.ColorIndex)))
}
