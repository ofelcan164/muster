package install

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func skillHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)
	return dir
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
	want := filepath.Join(dir, "skills", "muster-report", "SKILL.md")
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
	theirs := filepath.Join(dir, "skills", "their-own-skill")
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
	if _, err := os.Stat(filepath.Join(dir, "skills", "muster-report")); !os.IsNotExist(err) {
		t.Error("the skill directory survived the uninstall")
	}
	if _, err := os.Stat(filepath.Join(theirs, "SKILL.md")); err != nil {
		t.Errorf("uninstall took the user's own skill with it: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "skills")); err != nil {
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
	dir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	// Point the skill path somewhere that is not Muster's directory.
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
