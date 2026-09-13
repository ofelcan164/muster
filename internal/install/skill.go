package install

import (
	"bytes"
	"embed"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// The skill is embedded rather than read off disk. herdr invokes the binary
// from a working directory Muster does not control, and a skill that installs
// correctly only when the cwd happens to be right is one that fails silently.
// Embedding is also why the source sits under this package: go:embed cannot
// reach above the directory it is written in.
//
//go:embed skills/muster-report/SKILL.md
var skillFS embed.FS

const (
	skillName = "muster-report"
	skillSrc  = "skills/muster-report/SKILL.md"
	skillFile = "SKILL.md"
)

// harness is one agent runtime and where it keeps personal skills.
//
// detect is the directory whose existence says the runtime is installed. A
// harness that is not there gets nothing: creating ~/.codex for someone who has
// never run Codex would be Muster inventing a config directory on their behalf.
type harness struct {
	name   string
	detect string
	skills string
}

// harnesses is every runtime Muster knows how to reach, whether installed or
// not. Paths follow each tool's own convention, including OpenCode's singular
// `skill`, and honour the environment variables each one honours.
func harnesses() []harness {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	claude := filepath.Join(home, ".claude")
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		claude = d
	}
	codex := filepath.Join(home, ".codex")
	if d := os.Getenv("CODEX_HOME"); d != "" {
		codex = d
	}
	config := filepath.Join(home, ".config")
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		config = d
	}
	opencode := filepath.Join(config, "opencode")

	return []harness{
		{"Claude Code", claude, filepath.Join(claude, "skills")},
		{"Codex", codex, filepath.Join(codex, "skills")},
		{"OpenCode", opencode, filepath.Join(opencode, "skill")},
	}
}

// HarnessDirs names the directory each known runtime is detected by, for the
// message that has to say where Muster looked. Derived from harnesses() so the
// paths quoted are the paths actually searched, environment overrides included.
func HarnessDirs() []string {
	hs := harnesses()
	out := make([]string, 0, len(hs))
	for _, h := range hs {
		out = append(out, h.name+" ("+h.detect+")")
	}
	return out
}

// CanonicalSkillDir is the one real copy, shared across runtimes.
//
// ~/.agents/skills is the cross-tool convention, and every harness gets a
// symlink into it rather than a copy of its own. One file to update means a
// rewritten skill cannot go live in Claude Code and stale in Codex.
func CanonicalSkillDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agents", "skills", skillName)
}

// SkillPath is the file this installs. The links point at the directory holding
// it.
func SkillPath() string {
	dir := CanonicalSkillDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, skillFile)
}

// Skill installs the reporting skill: one canonical copy, then a link from
// every agent runtime that is actually installed.
//
// It carries its own trigger conditions in its description frontmatter rather
// than relying on a line in CLAUDE.md. Writing to CLAUDE.md was considered and
// rejected: it is a file the user curates, and a plugin editing it is a much
// bigger intrusion than dropping a self-contained directory next to the other
// skills.
//
// Safe to run repeatedly. It reports no change when the file is already what it
// would have written and every link already points where it should.
func Skill() (*Result, error) {
	canon := CanonicalSkillDir()
	if canon == "" {
		return nil, errors.New("cannot locate a home directory")
	}
	want, err := skillFS.ReadFile(skillSrc)
	if err != nil {
		return nil, err
	}
	path := SkillPath()
	res := &Result{Path: path}

	// The embedded copy is the version. A binary carrying a newer skill writes
	// different bytes, which is what makes a separate version sentinel file
	// unnecessary: the content already answers the question it would have.
	if existing, err := os.ReadFile(path); err != nil || string(existing) != string(want) {
		if err := writeAtomic(path, want, 0o644); err != nil {
			return nil, err
		}
		res.Changed = true
	}

	for _, h := range harnesses() {
		if _, err := os.Stat(h.detect); err != nil {
			continue
		}
		link := filepath.Join(h.skills, skillName)
		changed, err := linkSkill(canon, link)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", h.name, err)
		}
		res.Links = append(res.Links, link)
		res.Changed = res.Changed || changed
	}
	return res, nil
}

// linkSkill points one harness at the canonical copy.
//
// The target is relative and computed rather than spelled out, so it holds at
// whatever depth a runtime keeps its skills and survives a home directory that
// moves. A link that is already right is left alone; anything else, including
// the plain directory older versions of Muster wrote, is replaced.
func linkSkill(canon, link string) (bool, error) {
	target, err := filepath.Rel(filepath.Dir(link), canon)
	if err != nil {
		return false, err
	}
	if cur, err := os.Readlink(link); err == nil && cur == target {
		return false, nil
	}
	// A runtime whose skills directory is itself a symlink into ~/.agents/skills
	// makes link and canon one directory. Readlink does not see it, because the
	// symlink is the parent. Removing the link would then delete the canonical
	// copy this just wrote and leave a link pointing at itself.
	if aliasOfCanon(link, canon) {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return false, err
	}
	if _, err := removeOurs(link); err != nil {
		return false, err
	}
	if err := os.Symlink(target, link); err == nil {
		return true, nil
	}
	// Some filesystems have no symlinks. A copy goes stale the next time the
	// skill changes, which is worse than a link and better than no skill.
	return true, copySkill(canon, link)
}

// copySkill is the fallback for a filesystem without symlinks. Flat files only:
// the skill is one file, and quietly half-copying a tree would be worse than
// refusing.
func copySkill(src, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if e.IsDir() {
			return fmt.Errorf("%s holds a subdirectory, which the copy fallback cannot mirror", src)
		}
		b, err := os.ReadFile(filepath.Join(src, e.Name()))
		if err != nil {
			return err
		}
		if err := writeAtomic(filepath.Join(dst, e.Name()), b, 0o644); err != nil {
			return err
		}
	}
	return nil
}

// aliasOfCanon reports a path that is the canonical directory itself, reached
// through a symlinked parent, as when ~/.claude/skills links into
// ~/.agents/skills. Such a path only looks like a per-harness link. Removing it
// deletes the one real copy, so it is left alone in both directions.
//
// A path that is itself a symlink is excluded deliberately: that is an ordinary
// link of ours, and removing it takes the link and not its target.
func aliasOfCanon(path, canon string) bool {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink != 0 {
		return false
	}
	a, err := os.Stat(path)
	if err != nil {
		return false
	}
	b, err := os.Stat(canon)
	if err != nil {
		return false
	}
	return os.SameFile(a, b)
}

// removeOurs deletes a skill path, but only one Muster wrote, and reports
// whether anything was there. Owning the name muster-report is an assumption,
// not a fact: a user may have written a skill of their own under it, and
// RemoveAll on a name we merely expect to own is how an install eats someone
// else's work. A symlink is removed whatever it points at, since that destroys
// no content.
func removeOurs(path string) (bool, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if fi.Mode()&os.ModeSymlink == 0 && !isSkillDir(path) {
		return false, fmt.Errorf("refusing to remove %s: it is not Muster's skill", path)
	}
	if err := os.RemoveAll(path); err != nil {
		return false, err
	}
	return true, nil
}

// isSkillDir reports a directory holding this skill, identified by the name in
// its frontmatter. Every version of Muster has written that name, so a copy
// from an older install still counts as ours.
func isSkillDir(path string) bool {
	b, err := os.ReadFile(filepath.Join(path, skillFile))
	return err == nil && bytes.Contains(b, []byte("\nname: "+skillName+"\n"))
}

// RemoveSkill deletes the canonical copy and every link into it.
//
// The skill lives in a directory of its own for exactly this reason: removal is
// a handful of deletions, not a hunt through a shared file for lines that might
// have been ours. It removes only directories it named itself, never the skills
// directory around them, which belongs to the user.
func RemoveSkill() (*Result, error) {
	canon := CanonicalSkillDir()
	if canon == "" {
		return nil, errors.New("cannot locate a home directory")
	}
	res := &Result{Path: canon}

	for _, h := range harnesses() {
		link := filepath.Join(h.skills, skillName)
		// Removing a symlink takes the link, never what it points at, so the
		// canonical copy outlives the loop and gets removed once, below. The
		// exception is a skills directory that is a symlink into the canonical
		// one, where the link and canon are the same path; that is left for the
		// single removal below too.
		if aliasOfCanon(link, canon) {
			continue
		}
		gone, err := removeOurs(link)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", h.name, err)
		}
		res.Changed = res.Changed || gone
		if gone {
			res.Links = append(res.Links, link)
		}
	}

	gone, err := removeOurs(canon)
	if err != nil {
		return nil, err
	}
	res.Changed = res.Changed || gone
	return res, nil
}

// SkillDescription is the one-line trigger the skill carries, pulled out of the
// frontmatter so `muster install-skill` can show what it just installed.
func SkillDescription() string {
	b, err := skillFS.ReadFile(skillSrc)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		if rest, ok := strings.CutPrefix(line, "description:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// SkillInstalled reports whether the reporting skill is already in place.
//
// A path this cannot work out counts as installed. The only thing this answer
// drives is whether to offer the install, and offering it where it could not
// run is worse than staying quiet.
func SkillInstalled() bool {
	path := SkillPath()
	if path == "" {
		return true
	}
	_, err := os.Stat(path)
	return err == nil
}
