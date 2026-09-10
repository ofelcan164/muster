package install

import (
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
)

// SkillsDir is where Claude Code looks for personal skills.
//
// CLAUDE_CONFIG_DIR is honoured because Claude Code honours it, which also
// makes this testable without writing into anyone's real home directory.
func SkillsDir() string {
	if d := os.Getenv("CLAUDE_CONFIG_DIR"); d != "" {
		return filepath.Join(d, "skills")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".claude", "skills")
}

// skillPathForTest overrides SkillPath, so the refusal below can be exercised
// against a path this never would have produced.
var skillPathForTest string

// SkillPath is the file this installs.
func SkillPath() string {
	if skillPathForTest != "" {
		return skillPathForTest
	}
	dir := SkillsDir()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, skillName, "SKILL.md")
}

// Skill installs the reporting skill into the user's personal skills.
//
// It carries its own trigger conditions in its description frontmatter rather
// than relying on a line in CLAUDE.md. Writing to CLAUDE.md was considered and
// rejected: it is a file the user curates, and a plugin editing it is a much
// bigger intrusion than dropping a self-contained directory next to the other
// skills.
//
// Safe to run repeatedly. It owns one directory, writes one file, and reports
// no change when the file is already what it would have written.
func Skill() (*Result, error) {
	path := SkillPath()
	if path == "" {
		return nil, errors.New("cannot locate the skills directory")
	}
	want, err := skillFS.ReadFile(skillSrc)
	if err != nil {
		return nil, err
	}
	res := &Result{Path: path}

	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(want) {
		return res, nil
	}
	if err := writeAtomic(path, want, 0o644); err != nil {
		return nil, err
	}
	res.Changed = true
	return res, nil
}

// RemoveSkill deletes the installed skill.
//
// The skill lives in a directory of its own for exactly this reason: removal is
// one deletion, not a hunt through a shared file for lines that might have been
// ours. It removes only that directory, never the skills directory around it,
// which belongs to the user.
func RemoveSkill() (*Result, error) {
	path := SkillPath()
	if path == "" {
		return nil, errors.New("cannot locate the skills directory")
	}
	dir := filepath.Dir(path)

	// Refuse to delete anything that is not the directory this installed. A
	// wrong CLAUDE_CONFIG_DIR must not turn an uninstall into a recursive
	// delete of somewhere else.
	if filepath.Base(dir) != skillName {
		return nil, fmt.Errorf("refusing to remove %s: not Muster's skill directory", dir)
	}
	res := &Result{Path: dir}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return res, nil
		}
		return nil, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	res.Changed = true
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
