package mekbayproxy

import (
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/storage"
)

func TestMekBayHostParsing(t *testing.T) {
	id := storage.NewLinkID()
	for _, c := range []struct {
		host string
		id   string
		kind mekbayHostKind
	}{
		{"mekbay-" + id + ".battletech.zech.io", id, linkHost},
		{"mekbay-" + id + ".localhost:8080", id, linkHost},
		{"MekBay-" + strings.ToUpper(id) + ".Example.com", id, linkHost},
		{"mekbay-short.battletech.zech.io", "", linkHost},
		{"mekbay-" + id + "x.battletech.zech.io", "", linkHost},
		{"mekbay-" + strings.ReplaceAll(id, id[:1], "1") + ".x.io", "", linkHost}, // 1 is not a base32 character here
		{"mekbay.localhost:8080", "", sandboxHost},
		{"mekbay.battletech.zech.io", "", sandboxHost},
		{"battletech.zech.io", "", notMekBayHost},
		{"localhost:8080", "", notMekBayHost},
		{"notmekbay.example.com", "", notMekBayHost},
		{"x.mekbay.example.com", "", notMekBayHost},
		{"mekbayfoo.example.com", "", notMekBayHost},
	} {
		if gid, kind := parseMekBayHost(c.host); gid != c.id || kind != c.kind {
			t.Errorf("parseMekBayHost(%q) = %q, %v; want %q, %v", c.host, gid, kind, c.id, c.kind)
		}
	}
	if !IsHost("mekbay.localhost") || IsHost("battletech.zech.io") {
		t.Error("isMekBayHost follows the kind")
	}
	for host, want := range map[string]string{
		"mekbay-" + id + ".battletech.zech.io": "battletech.zech.io",
		"mekbay.localhost:8080":                "localhost:8080",
		"battletech.zech.io":                   "battletech.zech.io",
		"localhost:8080":                       "localhost:8080",
	} {
		if got := SiteHost(host); got != want {
			t.Errorf("siteHost(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestSwapUUID(t *testing.T) {
	const acct = testAccount
	for in, want := range map[string]string{
		`{"action":"x"}`:                      `{"action":"x"}`,
		`{"uuid":"a"}`:                        `{"uuid":"` + acct + `"}`,
		`{"a":1,"uuid":"a","b":{"uuid":"n"}}`: `{"a":1,"b":{"uuid":"n"},"uuid":"` + acct + `"}`,
		`[{"uuid":"a"}]`:                      `[{"uuid":"a"}]`,
		`{"note":"has \"uuid\" in text"}`:     `{"note":"has \"uuid\" in text"}`,
		`{"uuid":"a","data":{"n":1.50}}`:      `{"data":{"n":1.50},"uuid":"` + acct + `"}`,
	} {
		if got := string(swapUUID([]byte(in), acct)); got != want {
			t.Errorf("swapUUID(%s)\n got %s\nwant %s", in, got, want)
		}
	}
}
