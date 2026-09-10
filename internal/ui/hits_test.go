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
		byLine := map[int][]hitRegion{}
		for _, h := range m.hits {
			byLine[h.y] = append(byLine[h.y], h)
		}
		for y, hits := range byLine {
			for i := range hits {
				for j := i + 1; j < len(hits); j++ {
					a, b := hits[i], hits[j]
					if a.x0 <= b.x1 && b.x0 <= a.x1 {
						t.Errorf("width %d line %d: targets %d (%d-%d) and %d (%d-%d) overlap",
							w, y, a.target, a.x0, a.x1, b.target, b.x0, b.x1)
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
		for _, h := range m.hits {
			if h.y < 0 || h.y >= len(lines) {
				t.Errorf("width %d: region for target %d is on line %d, of %d drawn",
					w, h.target, h.y, len(lines))
			}
			if h.x0 < 0 || h.x1 > w-1 || h.x0 > h.x1 {
				t.Errorf("width %d: region for target %d spans %d-%d, outside 0-%d",
					w, h.target, h.x0, h.x1, w-1)
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
		for _, h := range m.hits {
			reached[h.target] = true
		}
		for i := 0; i < len(m.targets); i++ {
			if !reached[i] {
				t.Errorf("width %d: target %d (pane %q, ribbon %v) is drawn but has no click region",
					w, i, m.targetPane(i), m.isKind(i, kindRibbon))
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
		for _, h := range m.hits {
			for x := h.x0; x <= h.x1; x++ {
				got, ok := m.targetAt(x, h.y)
				if !ok {
					t.Fatalf("width %d: (%d,%d) is inside target %d's region but resolves to nothing",
						w, x, h.y, h.target)
				}
				if got != h.target {
					t.Fatalf("width %d: (%d,%d) belongs to target %d but resolves to %d",
						w, x, h.y, h.target, got)
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
		for _, h := range m.hits {
			ribbon := m.isKind(h.target, kindRibbon)
			if prev, seen := kind[h.y]; seen && prev != ribbon {
				t.Errorf("width %d line %d carries both a ribbon row and a grid card", w, h.y)
			}
			kind[h.y] = ribbon
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
	var hit hitRegion
	for _, h := range m.hits {
		if h.target == ti {
			hit = h
			break
		}
	}
	m.Update(tea.MouseMsg{X: hit.x0 + 1, Y: hit.y,
		Action: tea.MouseActionRelease, Button: tea.MouseButtonLeft})
	if got := m.Jump(); got != "acme/infra:p9" {
		t.Fatalf("one click on an agentless card jumped to %q, want its pane", got)
	}
}
