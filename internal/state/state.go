// Package state owns everything Muster persists: the daemon lock, the snapshot
// file, and the small amount of learned state that has to survive a restart.
//
// Learned state lives here and never in the plugin root, so a checkout of
// Muster contains no trace of anyone's repos.
package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"time"
)

// override is set by --state-dir. It exists so the daemon can be run outside a
// herdr plugin command during development without inventing a directory.
var override string

// SetDir points Muster at an explicit state directory.
func SetDir(dir string) { override = dir }

// ErrNoStateDir is returned when Muster has nowhere it has been told to write.
var ErrNoStateDir = errors.New(
	"no state directory: run through herdr, or pass --state-dir")

// BaseDir is the directory herdr gives the plugin, or the one --state-dir names.
//
// herdr sets HERDR_PLUGIN_STATE_DIR for every plugin command and creates the
// directory itself, so under herdr this is always the sanctioned location.
// There is deliberately no invented fallback: writing to a directory the user
// never asked for, that herdr does not know about and nothing cleans up, is not
// Muster's call to make. Outside a plugin context the caller must say where.
//
// herdr hands every session this same directory, so only what belongs to the
// user rather than to one session lives directly in it: the refusals.
func BaseDir() string {
	if override != "" {
		return override
	}
	return os.Getenv("HERDR_PLUGIN_STATE_DIR")
}

// Dir is the state directory for the herdr session this runs in.
//
// herdr names a session in HERDR_SESSION for every pane, plugin command and tab
// bar command it starts, and leaves it unset in the default session. Keyed on
// the base directory alone, a second session's overlay drew the first session's
// agents: both read one snapshot, written by whichever daemon started first.
// The default session keeps the base directory, so an install from before
// sessions were told apart finds its state where it left it.
//
// A name that is not a single path element is refused rather than folded into
// the base, which would mix two sessions again.
func Dir() string {
	base, session := BaseDir(), os.Getenv("HERDR_SESSION")
	switch {
	case base == "" || session == "" || session == "default":
		return base
	case session == "." || session == ".." || filepath.Base(session) != session:
		return ""
	}
	return filepath.Join(base, "sessions", session)
}

func EnsureDir() (string, error) {
	d := Dir()
	if d == "" {
		return "", ErrNoStateDir
	}
	if err := os.MkdirAll(d, 0o755); err != nil {
		return "", err
	}
	return d, nil
}

func SnapshotPath() string  { return filepath.Join(Dir(), "snapshot.json") }
func LockPath() string      { return filepath.Join(Dir(), "musterd.lock") }
func SpawnLockPath() string { return filepath.Join(Dir(), "musterd.spawn.lock") }
func LogPath() string       { return filepath.Join(Dir(), "musterd.log") }
func PersistPath() string   { return filepath.Join(Dir(), "state.json") }

// Persisted is the learned state that must survive a daemon restart.
type Persisted struct {
	// GridSlots pins each repo to a cell. Assigned in discovery order on first
	// sight and then never changed, because the whole value of the grid is that
	// a repo is in the same place every time you look.
	GridSlots map[string]int `json:"grid_slots"`

	// StatusSince remembers when each pane entered its current status. herdr
	// exposes no transition timestamp, so the daemon measures it; persisting it
	// stops every age resetting to zero when the daemon restarts.
	StatusSince map[string]StatusStamp `json:"status_since"`

	// LastDoneSeq is the state_change_seq at which a pane was last seen to
	// enter "done". The idle-never-done rule needs to know whether an idle
	// agent ever passed through done.
	LastDoneSeq map[string]uint64 `json:"last_done_seq"`

	// EverWorked records panes observed in the working state at least once.
	// Rank 5 turns on this: an agent you opened and never gave work to is idle,
	// not stalled, and flagging it forever is the noisiest thing the ribbon can
	// do.
	EverWorked map[string]bool `json:"ever_worked"`

	// FocusHistory is the recently focused agent panes, most recent first.
	// Two entries is all the back key needs.
	FocusHistory []string `json:"focus_history"`

	// LastProcess is the foreground process last seen in each non-agent pane.
	// Comparing against it is how a stopped dev server is detected: a fact,
	// unlike pattern matching a pane's text for something that looks like an
	// error.
	LastProcess map[string]string `json:"last_process"`

	// Stopped records panes whose process went away, what it was, and when.
	// Held until something runs in the pane again, or until it ages out.
	Stopped map[string]StoppedStamp `json:"stopped"`

	// TaskSeenAt records when the daemon first observed a given task token
	// value on a pane. herdr does not timestamp metadata tokens, so this is the
	// only way to tell a fresh task line from one written long ago.
	TaskSeenAt map[string]TaskStamp `json:"task_seen_at"`

	// LandedSeenAt records when the daemon first saw a pane's landed token match
	// its depends_on. The landed rule asks whether anyone moved after the landing,
	// and herdr keeps no time for it either.
	LandedSeenAt map[string]TaskStamp `json:"landed_seen_at"`

	// lastWritten is the last serialised form, used to skip redundant writes.
	lastWritten []byte
}

// TaskStamp is a task token value and when the daemon first saw it.
type TaskStamp struct {
	Value string    `json:"value"`
	Since time.Time `json:"since"`
}

// StoppedStamp is a process that went away and when it did.
type StoppedStamp struct {
	Process string    `json:"process"`
	At      time.Time `json:"at"`
}

type StatusStamp struct {
	Status string    `json:"status"`
	Since  time.Time `json:"since"`

	// Observed is true when the daemon watched this status begin, rather than
	// finding the pane already in it. It is persisted because the fact survives
	// a restart just as the timestamp does: an age learned honestly yesterday is
	// still honest today.
	Observed bool `json:"observed"`
}

func LoadPersisted() *Persisted {
	p := &Persisted{
		GridSlots:    map[string]int{},
		StatusSince:  map[string]StatusStamp{},
		LastDoneSeq:  map[string]uint64{},
		TaskSeenAt:   map[string]TaskStamp{},
		LandedSeenAt: map[string]TaskStamp{},
		LastProcess:  map[string]string{},
		EverWorked:   map[string]bool{},
		Stopped:      map[string]StoppedStamp{},
	}
	b, err := os.ReadFile(PersistPath())
	if err != nil {
		return p
	}
	// A corrupt state file must never stop the daemon: the worst case of
	// ignoring it is that grid slots get reassigned once.
	_ = json.Unmarshal(b, p)
	if p.GridSlots == nil {
		p.GridSlots = map[string]int{}
	}
	if p.StatusSince == nil {
		p.StatusSince = map[string]StatusStamp{}
	}
	if p.LastDoneSeq == nil {
		p.LastDoneSeq = map[string]uint64{}
	}
	if p.TaskSeenAt == nil {
		p.TaskSeenAt = map[string]TaskStamp{}
	}
	if p.LandedSeenAt == nil {
		p.LandedSeenAt = map[string]TaskStamp{}
	}
	if p.LastProcess == nil {
		p.LastProcess = map[string]string{}
	}
	if p.EverWorked == nil {
		p.EverWorked = map[string]bool{}
	}
	if p.Stopped == nil {
		p.Stopped = map[string]StoppedStamp{}
	}
	return p
}

// Save writes the learned state, but only when it has actually changed.
// Reconcile runs several times a second under event load and the state file
// usually looks identical each time.
func (p *Persisted) Save() error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	if bytes.Equal(b, p.lastWritten) {
		return nil
	}
	if err := WriteAtomic(PersistPath(), b); err != nil {
		return err
	}
	p.lastWritten = b
	return nil
}

// AssignSlot returns the grid cell for a repo key, allocating the lowest free
// cell the first time the key is seen and returning the same cell forever after.
func (p *Persisted) AssignSlot(key string) int {
	if s, ok := p.GridSlots[key]; ok {
		return s
	}
	used := make(map[int]bool, len(p.GridSlots))
	for _, s := range p.GridSlots {
		used[s] = true
	}
	slot := 0
	for used[slot] {
		slot++
	}
	p.GridSlots[key] = slot
	return slot
}

// WriteAtomic writes via a temporary file in the same directory and renames, so
// a reader never sees a half-written snapshot. The overlay reads this file on
// its hot path and must never have to handle a truncated parse.
//
// There is deliberately no fsync. Rename is what gives readers atomicity;
// fsync only buys surviving a power cut, and everything written here is a cache
// the daemon rebuilds in about 15ms. Syncing on every reconcile would mean a
// disk flush several times a second on a process whose whole point is to be
// cheap.
func WriteAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)

	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// UIStatePath is the overlay's own file, deliberately separate from
// PersistPath.
//
// The daemon holds Persisted in memory and rewrites it several times a second,
// so anything the overlay wrote there would survive until the next reconcile
// and no longer. Two writers, two files, no coordination needed.
func UIStatePath() string { return filepath.Join(Dir(), "ui.json") }

// UIState is what the overlay remembers between openings.
type UIState struct {
	// Sort is the mode cycled with s. The client is short-lived, so without
	// this every opening started over at first-seen.
	Sort int `json:"sort"`

	// Dismissed is the ribbon rows already acknowledged, as pane id to the
	// status the row was dismissed at. Keyed on the status because a status
	// change is what makes a row news again.
	Dismissed map[string]string `json:"dismissed,omitempty"`

	// Colors is palette slots picked with c, as repo key to slot. They beat
	// the hashed colour, which two repos can share.
	Colors map[string]int `json:"colors,omitempty"`

	// SayRows is how many lines the orchestrator strip gives its message at
	// rest, set by dragging the strip's rule. Zero is the default single line.
	SayRows int `json:"say_rows,omitempty"`

	lastWritten []byte
}

// LoadUI reads the overlay's state. A missing or corrupt file is not an error:
// the overlay opens with the default order, which is what it did before any of
// this existed.
func LoadUI() *UIState {
	u := &UIState{}
	b, err := os.ReadFile(UIStatePath())
	if err != nil {
		return u
	}
	_ = json.Unmarshal(b, u)
	u.lastWritten = b
	return u
}

// Save writes the fields this copy changed since it was loaded onto a fresh
// read of the file, and leaves every other field as the file has it.
//
// Two overlays can be open at once. Writing back a whole copy read at opening
// let one wipe what the other saved since: A saves its sort, B saves its order
// and puts the old sort back. The read and the write happen under a lock every
// overlay shares, so neither lands between the other's two. A new field needs
// its own line below or it is never saved.
func (u *UIState) Save() error {
	if _, err := EnsureDir(); err != nil {
		return err
	}
	l, err := LockBlocking(UIStatePath() + ".lock")
	if err != nil {
		return err
	}
	defer l.Release()

	var loaded UIState
	_ = json.Unmarshal(u.lastWritten, &loaded)
	fresh := LoadUI()
	if u.Sort != loaded.Sort {
		fresh.Sort = u.Sort
	}
	if !maps.Equal(u.Dismissed, loaded.Dismissed) {
		fresh.Dismissed = u.Dismissed
	}
	if !maps.Equal(u.Colors, loaded.Colors) {
		fresh.Colors = u.Colors
	}
	if u.SayRows != loaded.SayRows {
		fresh.SayRows = u.SayRows
	}
	b, err := json.Marshal(fresh)
	if err != nil {
		return err
	}
	if !bytes.Equal(b, fresh.lastWritten) {
		if err := WriteAtomic(UIStatePath(), b); err != nil {
			return err
		}
	}
	// Take on what the other overlay saved, so this copy's next save is measured
	// against the file as it now is rather than as it was at opening.
	*u = *fresh
	u.lastWritten = b
	return nil
}
