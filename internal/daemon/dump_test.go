package daemon

import (
	"io"
	"strings"
	"testing"
	"time"

	"github.com/ofelcan164/muster/internal/chain"
	"github.com/ofelcan164/muster/internal/model"
)

// web depends on api, which landed five minutes ago, and docs on neither end.
func showSnapshot() *model.Snapshot {
	return &model.Snapshot{Repos: []model.Repo{
		{Key: "acme/api", Name: "acme/api", Display: "api", Agents: []model.Agent{{
			PaneID: "w2:p1", Name: "endpoints", Status: model.StatusDone,
			Task: "add checkout endpoints", TaskSource: model.TaskFromOrchestrator,
		}}},
		{Key: "acme/web", Name: "acme/web", Display: "web", Agents: []model.Agent{{
			PaneID: "w3:p1", Name: "checkout-ui", Status: model.StatusIdle,
			Task: "checkout UI, waiting on api", TaskSource: model.TaskFromOrchestrator,
			DependsOn: "api#412", DependsOnRepo: "acme/api", LandedAt: time.Now().Add(-5 * time.Minute),
		}}},
		{Key: "acme/docs", Name: "acme/docs", Display: "docs", Agents: []model.Agent{{
			PaneID: "w4:p1", Name: "readme", Status: model.StatusWorking,
		}}},
	}}
}

func TestShowWithoutATargetListsBothEndsOfEveryEdge(t *testing.T) {
	var b strings.Builder
	c := &chain.Chain{Stages: chain.Parse("api > web"), SetBy: "orchestrator"}
	if err := Show(&b, showSnapshot(), c, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, want := range []string{
		"usual order  api > web  (set by orchestrator)",
		"web/checkout-ui  w3:p1",
		"depends on  api#412 · landed 5m ago",
		"api/endpoints  w2:p1  done",
		"needed by   web/checkout-ui (idle -)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("show does not say %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "docs/readme") {
		t.Errorf("an agent on no edge was listed:\n%s", out)
	}
}

func TestShowResolvesItsTarget(t *testing.T) {
	for target, want := range map[string]string{
		"w3:p1":           "web/checkout-ui",
		"web/checkout-ui": "web/checkout-ui",
		"WEB":             "web/checkout-ui",
		"acme/docs":       "docs/readme",
	} {
		var b strings.Builder
		if err := Show(&b, showSnapshot(), &chain.Chain{}, target, time.Now()); err != nil {
			t.Errorf("%s: %v", target, err)
			continue
		}
		if !strings.Contains(b.String(), want) {
			t.Errorf("show %s does not list %s:\n%s", target, want, b.String())
		}
	}
	if err := Show(io.Discard, showSnapshot(), &chain.Chain{}, "nope", time.Now()); err == nil {
		t.Error("a target that matches nothing was not an error")
	}
}
