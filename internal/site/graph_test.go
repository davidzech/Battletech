package site

import (
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
)

// graphScenarios is Refusal War's shape: 000 leads to 001, which offers a choice of 002 or 003, and both end there.
func graphScenarios() []content.Scenario {
	return []content.Scenario{
		{Slug: "scenario_000", Branches: []string{"scenario_001"}},
		{Slug: "scenario_001", Branches: []string{"scenario_002", "scenario_003"}},
		{Slug: "scenario_002"},
		{Slug: "scenario_003"},
	}
}

func TestScenarioGraph(t *testing.T) {
	scs := graphScenarios()
	c := &content.Campaign{Scenarios: scs}
	set := func(slugs ...string) map[string]bool {
		m := map[string]bool{}
		for _, s := range slugs {
			m[s] = true
		}
		return m
	}
	node := func(h string) string { // the row's node: kind and lane
		i := strings.Index(h, `<i class="sg-node `)
		return h[i+len(`<i class="sg-node `) : strings.Index(h[i:], `"></i>`)+i]
	}

	// Nothing done: 000 is offered, nothing else is reached, one lane.
	rows, kinds, lanes := scenarioGraph(scs, set(), c.Available(set()))
	if lanes != 1 || kinds[0] != graphNext || kinds[1] != graphLocked || strings.Contains(string(rows[0]), "<path") {
		t.Errorf("unstarted: %d lanes, %v", lanes, kinds)
	}

	// 000 and 001 done: a solid line 000 -> 001, then a dotted line on past both choices, each hanging off it on a stub in the second lane.
	done := set("scenario_000", "scenario_001")
	rows, kinds, lanes = scenarioGraph(scs, done, c.Available(done))
	if lanes != 2 || kinds[0] != graphDone || kinds[1] != graphDone || kinds[2] != graphNext || kinds[3] != graphNext {
		t.Errorf("a choice: %d lanes, %v", lanes, kinds)
	}
	for _, r := range []int{2, 3} {
		if got := node(string(rows[r])); got != `sg-next" style="--lane-n: 1` || !strings.Contains(string(rows[r]), `class="sg-next" d="M8 50H24"`) {
			t.Errorf("choice %d hangs off the line: %s", r, rows[r])
		}
	}
	if !strings.Contains(string(rows[2]), `class="sg-next" d="M8 50V100"`) || strings.Contains(string(rows[3]), `d="M8 50V100"`) {
		t.Errorf("the dotted line runs past 002 and ends at 003: %s / %s", rows[2], rows[3])
	}

	// One mission offered (after 000) is not a fork: it sits on the line.
	done = set("scenario_000")
	rows, kinds, lanes = scenarioGraph(scs, done, c.Available(done))
	if lanes != 1 || kinds[1] != graphNext || node(string(rows[1])) != `sg-next" style="--lane-n: 0` {
		t.Errorf("a single next mission: %d lanes, %s", lanes, rows[1])
	}

	// 000 -> 001 -> 003: the solid line runs straight past 002, and 002, the choice not taken, hangs off it, faded.
	done = set("scenario_000", "scenario_001", "scenario_003")
	rows, kinds, lanes = scenarioGraph(scs, done, c.Available(done))
	if lanes != 2 || kinds[2] != graphPassed || kinds[3] != graphDone {
		t.Errorf("002 passed over: %d lanes, %v", lanes, kinds)
	}
	if got := node(string(rows[2])); got != `sg-passed" style="--lane-n: 1` {
		t.Errorf("002's node: %s", got)
	}
	if got := node(string(rows[3])); got != `sg-done" style="--lane-n: 1` || !strings.Contains(string(rows[3]), `class="sg-done" d="M8 50H24"`) {
		t.Errorf("003, the option chosen, keeps its place beside the line on a solid stub: %s", rows[3])
	}
	if got := node(string(rows[1])); got != `sg-done" style="--lane-n: 0` {
		t.Errorf("001, the only way on from 000, sits on the line: %s", got)
	}
	if !strings.Contains(string(rows[2]), `class="sg-done" d="M8 0V50"`) || !strings.Contains(string(rows[2]), `class="sg-done" d="M8 50V100"`) || !strings.Contains(string(rows[2]), `class="sg-passed" d="M8 50H24"`) {
		t.Errorf("the path runs straight through 002's row, with 002 on a stub: %s", rows[2])
	}
}
