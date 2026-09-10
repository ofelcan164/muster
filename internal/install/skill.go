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

// skillPathForTest overrides SkillPath, so the refusal below can be exercised
// against a path this never would have produced.
var skillPathForTest string

// CanonicalSkillDir is the one real copy, shared across runtimes.
//
// ~/.agents/skills is the cross-tool convention, and every harness gets a
// symlink into it rather than a copy of its own. One file to update means a
// rewritten skill cannot go live in Claude Code and stale in Codex.
func CanonicalSkillDir() string {
	if skillPathForTest != "" {
		return filepath.Dir(skillPathForTest)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".agents", "skills", skillName)
}

// SkillPath is the file this installs. The links point at the directory holding
// it.
func SkillPath() string {
	if skillPathForTest != "" {
		return skillPathForTest
	}
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
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		return false, err
	}
	if err := os.RemoveAll(link); err != nil {
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

	// Refuse to delete anything that is not the directory this installed. A
	// wrong CLAUDE_CONFIG_DIR must not turn an uninstall into a recursive
	// delete of somewhere else.
	if filepath.Base(canon) != skillName {
		return nil, fmt.Errorf("refusing to remove %s: not Muster's skill directory", canon)
	}

	for _, h := range harnesses() {
		link := filepath.Join(h.skills, skillName)
		// os.Remove takes the link, never what it points at, so the canonical
		// copy outlives the loop and gets removed once, below.
		gone, err := removeIfPresent(link)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", h.name, err)
		}
		res.Changed = res.Changed || gone
		if gone {
			res.Links = append(res.Links, link)
		}
	}

	gone, err := removeIfPresent(canon)
	if err != nil {
		return nil, err
	}
	res.Changed = res.Changed || gone
	return res, nil
}

// removeIfPresent deletes a path if it is there, reporting whether it was.
// Lstat, not Stat: a link whose target is already gone still has to come out.
func removeIfPresent(path string) (bool, error) {
	if _, err := os.Lstat(path); err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := os.RemoveAll(path); err != nil {
		return false, err
	}
	return true, nil
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
