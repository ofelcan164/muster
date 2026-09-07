package ui

import (
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Mouse coordinates are the one thing about this overlay I cannot verify from
// here: a unit test calls the handler directly, so it proves the logic and says
// nothing about whether the coordinates herdr delivers are pane-local or
// screen-absolute. If they carry the sidebar's offset, hover lands on the wrong
// column and every test still passes.
//
// Setting MUSTER_MOUSE_DEBUG to a path makes the overlay record what it
// actually receives, next to what it resolved that to.
var mouseDebug = os.Getenv("MUSTER_MOUSE_DEBUG")

func (m *Model) logMouse(msg tea.MouseMsg, target int, ok bool) {
	if mouseDebug == "" {
		return
	}
	f, err := os.OpenFile(mouseDebug, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()

	what := "none"
	if ok {
		what = fmt.Sprintf("target=%d pane=%q ribbon=%v",
			target, m.TargetPane(target), m.IsRibbonTarget(target))
	}
	rows := 0
	if len(m.hits) > 0 {
		rows = m.hits[len(m.hits)-1].y
	}
	fmt.Fprintf(f, "%s action=%v button=%v x=%d y=%d -> %s  (view %dx%d, regions=%d, lastRow=%d)\n",
		time.Now().Format("15:04:05.000"), msg.Action, msg.Button,
		msg.X, msg.Y, what, m.width, m.height, len(m.hits), rows)
}
