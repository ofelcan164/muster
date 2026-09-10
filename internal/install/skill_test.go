package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// skillHome gives the test its own home with Claude Code "installed" in it, and
// nothing else, so the harness loop finds exactly one target.
func skillHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(dir, "no-codex"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "no-config"))
	if err := os.MkdirAll(filepath.Join(dir, ".claude", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// claudeLink is where Claude Code should end up pointing.
func claudeLink(home string) string {
	return filepath.Join(home, ".claude", "skills", skillName)
}

func TestSkillInstalls(t *testing.T) {
	dir := skillHome(t)

	res, err := Skill()
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if !res.Changed {
		t.Error("a first install reported no change")
	}
	want := filepath.Join(dir, ".agents", "skills", skillName, skillFile)
	if res.Path != want {
		t.Errorf("installed at %s, wanted %s", res.Path, want)
	}
	body, err := os.ReadFile(want)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\nname: muster-report\n") {
		t.Errorf("the installed skill has no frontmatter:\n%.80s", body)
	}
}

// The user vetoed a CLAUDE.md line, so the description is the only thing that
// gets the skill loaded. It has to name the moments it applies to.
func TestSkillDescriptionCarriesItsOwnTriggers(t *testing.T) {
	d := SkillDescription()
	if d == "" {
		t.Fatal("the skill has no description")
	}
	for _, want := range []string{"dispatch", "herdr", "Muster"} {
		if !strings.Contains(d, want) {
			t.Errorf("the description does not mention %q:\n%s", want, d)
		}
	}
}

// The skill has to name every token the daemon actually reads, or it teaches
// the orchestrator to write something Muster ignores.
func TestSkillDocumentsTheTokensMusterReads(t *testing.T) {
	b, err := skillFS.ReadFile(skillSrc)
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{"task", "blocked_on", "note", "role"} {
		if !strings.Contains(string(b), "`"+token+"`") {
			t.Errorf("the skill does not document the %q token", token)
		}
	}
	if !strings.Contains(string(b), "report-metadata") {
		t.Error("the skill does not name the command that writes the tokens")
	}
}

func TestSecondInstallReportsNoChange(t *testing.T) {
	skillHome(t)
	if _, err := Skill(); err != nil {
		t.Fatal(err)
	}
	res, err := Skill()
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("installing twice reported a change the second time")
	}
}

// An older version has to be replaced, not left in place.
func TestInstallOverwritesAnOlderSkill(t *testing.T) {
	skillHome(t)
	if _, err := Skill(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(SkillPath(), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Skill()
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed {
		t.Error("a stale skill was left in place")
	}
	body, _ := os.ReadFile(SkillPath())
	if strings.Contains(string(body), "stale") {
		t.Error("the stale content survived")
	}
}

// After uninstalling there must be nothing left that only makes sense with
// Muster installed, and nothing of the user's may go with it.
func TestUninstallLeavesNothingBehindAndTakesNothingElse(t *testing.T) {
	dir := skillHome(t)
	theirs := filepath.Join(dir, ".claude", "skills", "their-own-skill")
	if err := os.MkdirAll(theirs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theirs, "SKILL.md"), []byte("theirs\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Skill(); err != nil {
		t.Fatal(err)
	}

	res, err := RemoveSkill()
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !res.Changed {
		t.Error("removing an installed skill reported no change")
	}
	if _, err := os.Lstat(claudeLink(dir)); !os.IsNotExist(err) {
		t.Error("the Claude Code link survived the uninstall")
	}
	if _, err := os.Stat(CanonicalSkillDir()); !os.IsNotExist(err) {
		t.Error("the canonical copy survived the uninstall")
	}
	if _, err := os.Stat(filepath.Join(theirs, "SKILL.md")); err != nil {
		t.Errorf("uninstall took the user's own skill with it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "skills")); err != nil {
		t.Errorf("uninstall removed the skills directory itself: %v", err)
	}
}

func TestUninstallOnAFreshMachineIsQuiet(t *testing.T) {
	skillHome(t)
	res, err := RemoveSkill()
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if res.Changed {
		t.Error("removing a skill that was never installed reported a change")
	}
}

// A wrong CLAUDE_CONFIG_DIR must not turn an uninstall into a recursive delete
// of somewhere else.
func TestRemoveRefusesADirectoryThatIsNotOurs(t *testing.T) {
	dir := skillHome(t)

	// Point the canonical path somewhere that is not Muster's directory.
	orig := skillPathForTest
	skillPathForTest = filepath.Join(dir, "skills", "something-else", "SKILL.md")
	t.Cleanup(func() { skillPathForTest = orig })

	if err := os.MkdirAll(filepath.Dir(skillPathForTest), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := RemoveSkill(); err == nil {
		t.Error("remove accepted a directory that is not Muster's")
	}
	if _, err := os.Stat(filepath.Dir(skillPathForTest)); err != nil {
		t.Error("remove deleted a directory it should have refused")
	}
}

// The point of the canonical copy is that every runtime reads the same file. A
// link that resolves somewhere else, or a second copy, defeats it.
func TestSkillLinksHarnessesAtTheCanonicalCopy(t *testing.T) {
	dir := skillHome(t)

	res, err := Skill()
	if err != nil {
		t.Fatal(err)
	}
	link := claudeLink(dir)
	if len(res.Links) != 1 || res.Links[0] != link {
		t.Fatalf("links = %v, want just %s", res.Links, link)
	}

	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("Claude Code got %s, which is not a link: %v", link, err)
	}
	if target != filepath.Join("..", "..", ".agents", "skills", skillName) {
		t.Errorf("link target %q is not relative to the home directory", target)
	}
	body, err := os.ReadFile(filepath.Join(link, skillFile))
	if err != nil {
		t.Fatalf("the link does not resolve to a readable skill: %v", err)
	}
	canon, _ := os.ReadFile(SkillPath())
	if string(body) != string(canon) {
		t.Error("the link resolves to something other than the canonical copy")
	}
}

// Older versions of Muster wrote a plain directory into ~/.claude/skills.
// Upgrading has to replace it, or the user keeps reading a copy that no longer
// tracks the embedded skill.
func TestSkillReplacesAPlainDirectoryFromAnOlderInstall(t *testing.T) {
	dir := skillHome(t)
	old := claudeLink(dir)
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(old, skillFile), []byte("stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Skill(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(old); err != nil {
		t.Fatalf("the old directory was left in place: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(old, skillFile))
	if strings.Contains(string(body), "stale") {
		t.Error("the stale copy survived")
	}
}

// Every installed runtime gets a link, and one that is not installed gets
// nothing: Muster must not create ~/.codex for someone who has never run Codex.
func TestSkillOnlyTouchesRuntimesThatAreInstalled(t *testing.T) {
	dir := skillHome(t)
	codex := filepath.Join(dir, ".codex")
	t.Setenv("CODEX_HOME", codex)
	if err := os.MkdirAll(codex, 0o755); err != nil {
		t.Fatal(err)
	}
	opencode := filepath.Join(dir, "no-config", "opencode")

	res, err := Skill()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Links) != 2 {
		t.Fatalf("links = %v, want Claude Code and Codex", res.Links)
	}
	if _, err := os.Readlink(filepath.Join(codex, "skills", skillName)); err != nil {
		t.Errorf("Codex was installed but got no link: %v", err)
	}
	if _, err := os.Stat(opencode); !os.IsNotExist(err) {
		t.Error("created an OpenCode directory for a runtime that is not installed")
	}
}

// The startup path calls this on every overlay open, so a rerun that rewrote
// the links would churn the user's skills directory for nothing.
func TestSecondInstallLeavesTheLinkAlone(t *testing.T) {
	dir := skillHome(t)
	if _, err := Skill(); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(claudeLink(dir))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Skill()
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Error("a rerun reported a change")
	}
	after, err := os.Lstat(claudeLink(dir))
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Error("a rerun recreated the link")
	}
}
