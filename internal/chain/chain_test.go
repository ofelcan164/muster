package chain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	got := Parse("contracts > api > web,mobile")
	want := [][]string{{"contracts"}, {"api"}, {"web", "mobile"}}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("stage %d: got %v, want %v", i, got[i], want[i])
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("stage %d: got %v, want %v", i, got[i], want[i])
			}
		}
	}
}

func TestParseTolerantOfSpacing(t *testing.T) {
	for _, spec := range []string{
		"contracts>api>web,mobile",
		"  contracts  >  api  >  web , mobile  ",
		"contracts > api > web,mobile,",
	} {
		if got := Format(Parse(spec)); got != "contracts > api > web,mobile" {
			t.Errorf("Parse(%q) formatted to %q", spec, got)
		}
	}
}

func TestParseEmpty(t *testing.T) {
	if got := Parse(""); len(got) != 0 {
		t.Errorf("empty spec should give no stages, got %v", got)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	write := func(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }

	in := &Chain{Stages: Parse("contracts > api > web,mobile"), SetBy: "orchestrator"}
	if err := Save(dir, in, write); err != nil {
		t.Fatal(err)
	}
	out := Load(dir)
	if Format(out.Stages) != Format(in.Stages) {
		t.Errorf("stages did not survive: %v", out.Stages)
	}
	if out.SetBy != "orchestrator" {
		t.Errorf("SetBy = %q", out.SetBy)
	}
	if out.SetAt.IsZero() {
		t.Error("Save should stamp SetAt so a later orchestrator can judge its age")
	}
}

// A missing or corrupt file must not break anything; it just means no order.
func TestLoadToleratesMissingAndCorrupt(t *testing.T) {
	if c := Load(t.TempDir()); !c.Empty() {
		t.Error("missing order should load empty")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chain.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := Load(dir); !c.Empty() {
		t.Error("corrupt order should load empty rather than fail")
	}
}
