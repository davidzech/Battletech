package site

import (
	"testing"

	"github.com/davidzech/Battletech/internal/content"
)

func TestEveryScenarioForceResolves(t *testing.T) {
	lib := content.Library{Root: "../../campaigns"}
	cs, err := lib.Campaigns()
	if err != nil {
		t.Fatal(err)
	}
	for i := range cs {
		for j := range cs[i].Scenarios {
			sc := &cs[i].Scenarios[j]
			if len(sc.Forces) == 0 {
				continue
			}
			d, err := opForDraft(cs[i], sc)
			if err != nil {
				t.Errorf("%s/%s: %v", cs[i].Slug, sc.Slug, err)
				continue
			}
			if len(d.Groups) != len(sc.Forces) {
				t.Errorf("%s: %d groups for %d force files", sc.Slug, len(d.Groups), len(sc.Forces))
			}
		}
	}
}
