package daemon

import "testing"

// Captured verbatim from a live Claude Code agent that herdr had marked
// blocked, on 2026-09-06. This is the shape the extractor actually has to
// handle, rather than one invented to match the code.
const claudeCodePrompt = `
❯ write a file called abc.txt with the alphabet in
  it and then delete it after you create it

● Write(abc.txt)

─────────────────────────────────────────────────────
 Create file
 abc.txt
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
  1 abcdefghijklmnopqrstuvwxyz
╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌╌
 Do you want to create abc.txt?
 ❯ 1. Yes
   2. Yes, and switch to accept edits (auto-approve
      file edits and common file commands) for this
      session (shift+tab)
   3. No

 Esc to cancel · Tab to amend
`

func TestExtractsTheRealClaudeCodePrompt(t *testing.T) {
	got := ExtractQuestion(claudeCodePrompt)
	if got != "Do you want to create abc.txt?" {
		t.Errorf("got %q", got)
	}
}

// The file being written contains a line of text that is not a question, and
// the user's own prompt is above it. Neither should win.
func TestPrefersTheQuestionAboveTheChoices(t *testing.T) {
	text := `
● Bash(rm -rf build)

 Is this really what you wanted?
 Do you want to run rm -rf build?
 ❯ 1. Yes
   2. No
`
	if got := ExtractQuestion(text); got != "Do you want to run rm -rf build?" {
		t.Errorf("the question nearest the choices should win, got %q", got)
	}
}

// Not every agent offers numbered options, so a bare question still counts.
func TestFallsBackToTheLastQuestion(t *testing.T) {
	text := "some output\nProceed with deploy?\n"
	if got := ExtractQuestion(text); got != "Proceed with deploy?" {
		t.Errorf("got %q", got)
	}
}

func TestNoQuestionFound(t *testing.T) {
	for _, text := range []string{"", "just output\nno prompt here\n", "\n\n"} {
		if got := ExtractQuestion(text); got != "" {
			t.Errorf("ExtractQuestion(%q) = %q, want empty", text, got)
		}
	}
}

func TestIsChoice(t *testing.T) {
	for _, s := range []string{" ❯ 1. Yes", "   2. No", "3) Maybe", "1. Yes"} {
		if !isChoice(s) {
			t.Errorf("%q should read as a choice", s)
		}
	}
	for _, s := range []string{"", "Yes", " 1 abcdefghijklmnopqrstuvwxyz", "─────"} {
		if isChoice(s) {
			t.Errorf("%q should not read as a choice", s)
		}
	}
}
