package chain

import (
	"os"
	"path/filepath"
	"testing"
)

func linear() *Chain {
	return &Chain{
		Stages:      [][]string{{"contracts"}, {"api"}, {"web", "mobile"}},
		Independent: []string{"infra"},
	}
}

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

func TestDependsOnFollowsStageOrder(t *testing.T) {
	c := linear()
	if !c.DependsOn("web", "api") {
		t.Error("web is after api and should depend on it")
	}
	if !c.DependsOn("web", "contracts") {
		t.Error("dependency should reach back through the whole chain")
	}
	if c.DependsOn("api", "web") {
		t.Error("api is upstream of web and must not depend on it")
	}
}

// Repos in the same stage run in parallel, so neither waits on the other.
func TestSameStageIsNotADependency(t *testing.T) {
	c := linear()
	if c.DependsOn("web", "mobile") || c.DependsOn("mobile", "web") {
		t.Error("parallel repos must not depend on each other")
	}
}

// This is the false positive the chain exists to remove.
func TestIndependentDependsOnNothing(t *testing.T) {
	c := linear()
	if c.DependsOn("infra", "api") || c.DependsOn("api", "infra") {
		t.Error("an independent repo takes part in no ordering")
	}
	if !c.IsIndependent("infra") {
		t.Error("infra should be independent")
	}
}

func TestUnknownRepoDependsOnNothing(t *testing.T) {
	c := linear()
	if c.DependsOn("docs", "api") || c.DependsOn("api", "docs") {
		t.Error("a repo not in the chain must not produce dependencies")
	}
}

// The orchestrator should not have to know whether Muster resolved a repo to
// "api" or "acme/api".
func TestOwnerPrefixIsToleratedEitherWay(t *testing.T) {
	c := &Chain{Stages: [][]string{{"contracts"}, {"acme/api"}}}
	if !c.DependsOn("acme/api", "acme/contracts") {
		t.Error("configured short name should match a discovered owner/name")
	}
	if !c.DependsOn("api", "contracts") {
		t.Error("configured owner/name should match a discovered short name")
	}
}

func TestEmptyChain(t *testing.T) {
	var c Chain
	if !c.Empty() {
		t.Error("a zero chain is empty")
	}
	if c.DependsOn("web", "api") {
		t.Error("an empty chain implies no dependencies")
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	write := func(p string, b []byte) error { return os.WriteFile(p, b, 0o644) }

	in := linear()
	in.SetBy = "orchestrator"
	if err := Save(dir, in, write); err != nil {
		t.Fatal(err)
	}
	out := Load(dir)
	if Format(out.Stages) != Format(in.Stages) {
		t.Errorf("stages did not survive: %v", out.Stages)
	}
	if len(out.Independent) != 1 || out.Independent[0] != "infra" {
		t.Errorf("independent did not survive: %v", out.Independent)
	}
	if out.SetBy != "orchestrator" {
		t.Errorf("SetBy = %q", out.SetBy)
	}
	if out.SetAt.IsZero() {
		t.Error("Save should stamp SetAt so a later orchestrator can judge its age")
	}
}

// A missing or corrupt chain must not break anything; it just means no gates.
func TestLoadToleratesMissingAndCorrupt(t *testing.T) {
	if c := Load(t.TempDir()); !c.Empty() {
		t.Error("missing chain should load empty")
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chain.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := Load(dir); !c.Empty() {
		t.Error("corrupt chain should load empty rather than fail")
	}
}
