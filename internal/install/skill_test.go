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
	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(dir, "state"))
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
	for _, token := range []string{"task", "blocked_on", "landed", "note", "role"} {
		if !strings.Contains(string(b), "`"+token+"`") {
			t.Errorf("the skill does not document the %q token", token)
		}
	}
	if !strings.Contains(string(b), "report-metadata") {
		t.Error("the skill does not name the command that writes the tokens")
	}
}

// An agent pane has neither Muster on PATH nor its state dir, so the installed
// skill carries both. A new path rewrites it, and with no state dir there is no
// command to write, so there is no skill either.
func TestSkillCarriesTheCommandThatReachesMuster(t *testing.T) {
	dir := skillHome(t)
	// One skill serves every session, and the pane that runs it names its own,
	// so the command carries the base dir even when installed from a session.
	t.Setenv("HERDR_SESSION", "demo")
	if _, err := Skill(); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(SkillPath())
	if err != nil {
		t.Fatal(err)
	}
	bin, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	want := "'" + bin + "' --state-dir '" + filepath.Join(dir, "state") + "' show"
	if !strings.Contains(string(body), want) {
		t.Errorf("the skill does not carry %q", want)
	}
	if strings.Contains(string(body), commandPlaceholder) {
		t.Error("the placeholder survived into the installed skill")
	}

	t.Setenv("HERDR_PLUGIN_STATE_DIR", filepath.Join(dir, "moved"))
	if res, err := Skill(); err != nil || !res.Changed {
		t.Errorf("a new state dir did not rewrite the skill: %v", err)
	}

	t.Setenv("HERDR_PLUGIN_STATE_DIR", "")
	if _, err := Skill(); err == nil {
		t.Error("installed a skill with no command to put in it")
	}
}

// Declining the skill is not declining the keys, or the reverse.
func TestSkillRefusalHasItsOwnMarker(t *testing.T) {
	dir := t.TempDir()
	if err := SetSkillOptOut(dir, true); err != nil {
		t.Fatal(err)
	}
	if !SkillOptedOut(dir) || OptedOut(dir) {
		t.Errorf("skill refused = %v, keys refused = %v; want true, false", SkillOptedOut(dir), OptedOut(dir))
	}
	if err := SetSkillOptOut(dir, false); err != nil {
		t.Fatal(err)
	}
	if SkillOptedOut(dir) {
		t.Error("installing again did not clear the refusal")
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

// Owning the name muster-report is an assumption. A user who wrote a skill of
// their own under it keeps it, through both an install and an uninstall.
func TestSkillRefusesAForeignDirectoryOfTheSameName(t *testing.T) {
	dir := skillHome(t)
	theirs := claudeLink(dir)
	if err := os.MkdirAll(theirs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(theirs, skillFile), []byte("their own notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Skill(); err == nil {
		t.Error("install replaced a skill directory it did not write")
	}
	if _, err := RemoveSkill(); err == nil {
		t.Error("uninstall removed a skill directory it did not write")
	}
	body, err := os.ReadFile(filepath.Join(theirs, skillFile))
	if err != nil || string(body) != "their own notes\n" {
		t.Errorf("the user's own skill was modified: %q, %v", body, err)
	}
}

// ~/.claude/skills symlinked into ~/.agents/skills is a common setup. It makes
// the link and the canonical copy one directory, and replacing the link then
// deletes the copy it points at.
func TestSkillSurvivesASymlinkedSkillsDirectory(t *testing.T) {
	dir := skillHome(t)
	agents := filepath.Join(dir, ".agents", "skills")
	if err := os.MkdirAll(agents, 0o755); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(dir, ".claude", "skills")
	if err := os.RemoveAll(claude); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(agents, claude); err != nil {
		t.Fatal(err)
	}

	for i := range 2 {
		if _, err := Skill(); err != nil {
			t.Fatalf("install %d: %v", i+1, err)
		}
		body, err := os.ReadFile(SkillPath())
		if err != nil {
			t.Fatalf("install %d left no readable skill: %v", i+1, err)
		}
		if !strings.HasPrefix(string(body), "---\nname: muster-report\n") {
			t.Fatalf("install %d wrote something that is not the skill", i+1)
		}
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
	// What an older Muster wrote: the skill itself, frontmatter and all, as a
	// plain directory rather than a link. That frontmatter is what marks it as
	// ours to replace.
	if err := os.WriteFile(filepath.Join(old, skillFile), []byte("---\nname: muster-report\n---\nstale\n"), 0o644); err != nil {
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
