package site

import (
	"strings"
	"testing"

	"github.com/davidzech/Battletech/internal/content"
)

func TestRefusalWarPlayersOnHomePage(t *testing.T) {
	c, ok := (content.Library{Root: "../../campaigns"}).Campaign("refusalwar")
	if !ok || c.MinPlayers != 1 || c.MaxPlayers != 2 || c.PlayersLabel() != "1-2 players" {
		t.Fatalf("Refusal War should be for 1-2 players: %+v", c)
	}
	srv, _ := newTestServer(t)
	if home := body(t, srv, "/", nil); !strings.Contains(home, `<span class="sub">1-2 players</span>`) {
		t.Fatalf("the campaigns list should show the player range: %.1500s", home)
	}
}
