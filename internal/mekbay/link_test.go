package mekbay

import (
	"strings"
	"testing"
)

func TestParseHangar(t *testing.T) {
	// A pasted link is reduced to the hangar: https://mekbay.com/?instance=<id>.
	for name, in := range map[string]string{
		"the link as MekBay gives it":    goodHangar,
		"surrounding space and capitals": "  " + strings.ToUpper(goodHangar[:8]) + goodHangar[8:] + " ",
		"no unit selection":              "https://mekbay.com/?instance=" + goodInstance,
		"a garbled unit selection":       "https://mekbay.com/?instance=" + goodInstance + "&sel=nope",
		"tracking and other parameters":  "https://mekbay.com/?utm_source=x&instance=" + goodInstance + "&operation=abc&tab=Sheet",
		"a fragment":                     "https://mekbay.com/?instance=" + goodInstance + "#top",
		"the www host":                   "https://www.mekbay.com/?instance=" + goodInstance,
		"MekBay's next release":          "https://next.mekbay.com/?instance=" + goodInstance,
		"a path":                         "https://mekbay.com/units/?instance=" + goodInstance,
		"friendly and enemy forces":      "https://mekbay.com/?instance=" + goodInstance + ",enemy:01a12f98-8a21-7d12-b43d-52bb70872376&sel=" + goodSel,
		"the enemy listed first":         "https://mekbay.com/?instance=enemy:01a12f98-8a21-7d12-b43d-52bb70872376," + goodInstance,
		"several enemies around ours":    "https://mekbay.com/?instance=enemy:01a12f98-8a21-7d12-b43d-52bb70872376, " + goodInstance + ",enemy:01a12f98-8a21-7d12-b43d-52bb70872377",
	} {
		h, err := ParseHangarLink(in)
		if err != nil || h.Instance != goodInstance || h.URL != goodHangarURL {
			t.Errorf("%s: parseHangar(%q) = %+v, %v; want %s", name, in, h, err, goodHangarURL)
		}
	}
	for name, bad := range map[string]string{
		"nothing":                  "",
		"not a url":                "not a url",
		"not https":                "http://mekbay.com/?instance=" + goodInstance,
		"another site":             "https://evil.example/?instance=" + goodInstance,
		"a lookalike host":         "https://mekbay.com.evil.example/?instance=" + goodInstance,
		"a user in the link":       "https://user@mekbay.com/?instance=" + goodInstance,
		"a port":                   "https://mekbay.com:8443/?instance=" + goodInstance,
		"no instance":              "https://mekbay.com/",
		"a bad instance":           "https://mekbay.com/?instance=nope",
		"only enemy forces":        "https://mekbay.com/?instance=enemy:" + goodInstance,
		"a bad first friendly one": "https://mekbay.com/?instance=nope," + goodInstance,
		"an operation link":        "https://mekbay.com/?operation=01a1e329-5a80-76bb-8f8c-5443d79bcd02",
	} {
		if h, err := ParseHangarLink(bad); err == nil {
			t.Errorf("%s: parseHangar(%q) should fail, got %+v", name, bad, h)
		}
	}
	if _, err := ParseHangarLink("https://mekbay.com/?operation=01a1e329-5a80-76bb-8f8c-5443d79bcd02"); err == nil || !strings.Contains(err.Error(), "operation link") {
		t.Errorf("an operation link should say so, got %v", err)
	}
}
