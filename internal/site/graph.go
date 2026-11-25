package site

import (
	"fmt"
	"html/template"
	"strings"

	"github.com/davidzech/Battletech/internal/content"
)

// The campaign page draws the run's path left of its scenario list, on one line: a solid line through every completed mission (passing straight by a
// mission it skipped), then a dotted line on to the missions offered now. One mission offered sits on the line; a choice of several is a fork, so the dotted
// line runs on past all of them and each hangs off it on a short stub (├ ... └), a bracket rather than a tree. Once chosen, the option keeps its place beside
// the line (a green dot on a solid stub), and a choice not taken hangs off the solid line the same way, faded: a dot on the line is a mission that was the only
// way on, a dot beside it one option of a fork. Every scenario is a row; each row draws its own slice as an SVG that overhangs the gaps between the cards, so the slices join (also across
// a hidden row, since a hidden row takes no room).

// Line and node kinds, used as CSS classes.
const (
	graphDone   = "done"   // the path taken: a completed mission, and the line between two
	graphNext   = "next"   // a mission offered now, and the line to it
	graphPassed = "passed" // a mission offered earlier but not taken
	graphLocked = "locked" // a mission nothing has offered
)

// graphCell is one row's slice of the path.
type graphCell struct {
	top, bottom string // the line's kind in the row's top and bottom half ("" for none)
	stub        string // the kind of a stub from the line out to a node in the second lane ("" for none)
	node        string
	lane        int // 0: on the line; 1: beside it
}

// scenarioGraph lays out the path for a campaign's scenarios (in number order) and returns each row's drawing, each row's node kind and the number of lanes
// (1, or 2 when something sits beside the line).
func scenarioGraph(scs []content.Scenario, completed, available map[string]bool) ([]template.HTML, []string, int) {
	n := len(scs)
	row := map[string]int{}
	first, last := -1, -1
	for i, sc := range scs {
		row[sc.Slug] = i
		if completed[sc.Slug] {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	// The missions offered now: the last completed one's open branches (before anything is completed, the first mission, which has no line to it).
	isChoice := map[int]bool{}
	end := last // the dotted line runs from the last completed mission to the last choice
	for i, sc := range scs {
		if available[sc.Slug] && !completed[sc.Slug] && i > last {
			isChoice[i] = true
			if last >= 0 {
				end = i
			}
		}
	}
	fork := last >= 0 && len(isChoice) > 1
	// A completed mission that was one option of a fork: the completed mission before it offered more than one.
	chosen := map[int]bool{}
	prev := -1
	for i, sc := range scs {
		if !completed[sc.Slug] {
			continue
		}
		if prev >= 0 {
			options := 0
			for _, b := range scs[prev].Branches {
				if _, ok := row[b]; ok {
					options++
				}
			}
			chosen[i] = options > 1
		}
		prev = i
	}
	passed := map[int]bool{} // offered by a completed mission, not taken
	for i, sc := range scs {
		if !completed[sc.Slug] {
			continue
		}
		for _, b := range sc.Branches {
			if j, ok := row[b]; ok && j > i && !completed[b] && !available[b] {
				passed[j] = true
			}
		}
	}

	cells := make([]graphCell, n)
	kinds := make([]string, n)
	lanes := 1
	for r, sc := range scs {
		c := &cells[r]
		switch {
		case first >= 0 && r > first && r <= last:
			c.top = graphDone
		case last >= 0 && r > last && r <= end:
			c.top = graphNext
		}
		switch {
		case first >= 0 && r >= first && r < last:
			c.bottom = graphDone
		case last >= 0 && r >= last && r < end:
			c.bottom = graphNext
		}
		switch {
		case completed[sc.Slug] && chosen[r]:
			c.node, c.lane, c.stub = graphDone, 1, graphDone
		case completed[sc.Slug]:
			c.node = graphDone
		case isChoice[r] && fork:
			c.node, c.lane, c.stub = graphNext, 1, graphNext
		case isChoice[r]:
			c.node = graphNext
		case passed[r]:
			c.node, c.lane = graphPassed, 1
			if c.top != "" { // the line passes here: hang it off the line
				c.stub = graphPassed
			}
		default:
			c.node = graphLocked
			if c.top != "" || c.bottom != "" {
				c.lane = 1 // beside the line, not on it
			}
		}
		if c.lane > 0 {
			lanes = 2
		}
		kinds[r] = c.node
	}

	x := func(lane int) int { return 8 + 16*lane }
	out := make([]template.HTML, n)
	for r, c := range cells {
		var b strings.Builder
		fmt.Fprintf(&b, `<svg class="sgraph-lines" viewBox="0 0 %d 100" preserveAspectRatio="none" aria-hidden="true">`, 16*lanes)
		line := func(kind, d string) {
			fmt.Fprintf(&b, `<path class="sg-%s" d="%s" vector-effect="non-scaling-stroke"/>`, kind, d)
		}
		if c.top != "" {
			line(c.top, fmt.Sprintf("M%d 0V50", x(0)))
		}
		if c.bottom != "" {
			line(c.bottom, fmt.Sprintf("M%d 50V100", x(0)))
		}
		if c.stub != "" {
			line(c.stub, fmt.Sprintf("M%d 50H%d", x(0), x(1)))
		}
		b.WriteString(`</svg>`)
		fmt.Fprintf(&b, `<i class="sg-node sg-%s" style="--lane-n: %d"></i>`, c.node, c.lane)
		out[r] = template.HTML(b.String())
	}
	return out, kinds, lanes
}
