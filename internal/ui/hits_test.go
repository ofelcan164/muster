package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// widths covers both ends of what an overlay actually gets: a fullscreen 13"
// laptop is 143 columns of tab area, the same laptop split is 53, and the
// column breakpoints sit between them.
var widths = []int{40, 53, 69, 70, 100, 119, 120, 143, 199, 200, 240}

// rendered returns a model that has drawn itself, which is the only state in
// which its hit regions mean anything.
func rendered(t *testing.T, width int) (*Model, []string) {
	t.Helper()
	m := New(testSnapshot(), "")
	out, _ := m.Update(tea.WindowSizeMsg{Width: width, Height: 40})
	return m, strings.Split(out.View(), "\n")
}

// Two regions on one line that both contain a point make the click resolve to
// whichever was recorded first, which is a coin toss the user cannot see.
func TestHitRegionsNeverOverlap(t *testing.T) {
	for _, w := range widths {
		m, _ := rendered(t, w)
		byLine := map[int][]Hit{}
		for _, h := range m.Hits() {
			byLine[h.Y] = append(byLine[h.Y], h)
		}
		for y, hits := range byLine {
			for i := range hits {
				for j := i + 1; j < len(hits); j++ {
					a, b := hits[i], hits[j]
					if a.X0 <= b.X1 && b.X0 <= a.X1 {
						t.Errorf("width %d line %d: targets %d (%d-%d) and %d (%d-%d) overlap",
							w, y, a.Target, a.X0, a.X1, b.Target, b.X0, b.X1)
					}
				}
			}
		}
	}
}

// A region outside the drawn area can never be clicked, and one that runs past
// the width points at columns the pane does not have.
func TestHitRegionsStayInsideTheFrame(t *testing.T) {
	for _, w := range widths {
		m, lines := rendered(t, w)
		for _, h := range m.Hits() {
			if h.Y < 0 || h.Y >= len(lines) {
				t.Errorf("width %d: region for target %d is on line %d, of %d drawn",
					w, h.Target, h.Y, len(lines))
			}
			if h.X0 < 0 || h.X1 > w-1 || h.X0 > h.X1 {
				t.Errorf("width %d: region for target %d spans %d-%d, outside 0-%d",
					w, h.Target, h.X0, h.X1, w-1)
			}
		}
	}
}

// Every card the grid draws has to be clickable, in every column. Regions used
// to be recorded without a column offset, which left everything but the first
// column drawn and dead.
func TestEveryTargetIsClickableAtEveryWidth(t *testing.T) {
	for _, w := range widths {
		m, _ := rendered(t, w)
		reached := map[int]bool{}
		for _, h := range m.Hits() {
			reached[h.Target] = true
		}
		for i := 0; i < m.TargetCount(); i++ {
			if !reached[i] {
				t.Errorf("width %d: target %d (pane %q, ribbon %v) is drawn but has no click region",
					w, i, m.TargetPane(i), m.IsRibbonTarget(i))
			}
		}
	}
}

// Clicking a card must resolve to that card. Walking every cell of every region
// is what catches an off-by-one in the column offset, which a handler called
// with coordinates the model itself produced never would.
func TestEveryCellResolvesToItsOwnTarget(t *testing.T) {
	for _, w := range widths {
		m, _ := rendered(t, w)
		for _, h := range m.Hits() {
			for x := h.X0; x <= h.X1; x++ {
				got, ok := m.targetAt(x, h.Y)
				if !ok {
					t.Fatalf("width %d: (%d,%d) is inside target %d's region but resolves to nothing",
						w, x, h.Y, h.Target)
				}
				if got != h.Target {
					t.Fatalf("width %d: (%d,%d) belongs to target %d but resolves to %d",
						w, x, h.Y, h.Target, got)
				}
			}
		}
	}
}

// Ribbon rows claim the full width and grid cards claim one cell, so a ribbon
// row sharing a line with a card would swallow the whole row.
func TestRibbonAndGridNeverShareALine(t *testing.T) {
	for _, w := range widths {
		m, _ := rendered(t, w)
		kind := map[int]bool{}
		for _, h := range m.Hits() {
			ribbon := m.IsRibbonTarget(h.Target)
			if prev, seen := kind[h.Y]; seen && prev != ribbon {
				t.Errorf("width %d line %d carries both a ribbon row and a grid card", w, h.Y)
			}
			kind[h.Y] = ribbon
		}
	}
}

// One click opens. A card with no agents still sits over panes, so clicking it
// must jump to one of them rather than only moving the selection.
func TestOneClickOpensAgentlessCard(t *testing.T) {
	m, _ := rendered(t, 143)
	ti := m.targetIndex("repo:acme/infra")
	if ti < 0 {
		t.Fatal("the agentless repo is not a target")
	}
	var hit Hit
	for _, h := range m.Hits() {
		if h.Target == ti {
			hit = h
			break
		}
	}
	m.Update(tea.MouseMsg{X: hit.X0 + 1, Y: hit.Y,
		Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if got := m.Jump(); got != "acme/infra:p9" {
		t.Fatalf("one click on an agentless card jumped to %q, want its pane", got)
	}
}
