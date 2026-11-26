package storage

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/davidzech/Battletech/internal/mekbay"
	"github.com/davidzech/Battletech/internal/pilot"
)

// atOnce runs n requests together and returns their errors.
func atOnce(n int, request func(i int) error) []error {
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			errs[i] = request(i)
		}()
	}
	close(start)
	wg.Wait()
	return errs
}

// oneWins checks that exactly one of the requests went through and every other was refused with want.
func oneWins(t *testing.T, what string, errs []error, want error) {
	t.Helper()
	won := 0
	for _, err := range errs {
		switch {
		case err == nil:
			won++
		case !errors.Is(err, want):
			t.Errorf("%s: a request that loses is refused with %q, got %v", what, want, err)
		}
	}
	if won != 1 {
		t.Errorf("%s: %d of %d requests went through, want exactly 1: %v", what, won, len(errs), errs)
	}
}

// pilotRaces makes the requests that check and then write (recruit at the limit, a name, a portrait, a revive, Edge abilities, starting a campaign) several at
// once. This only proves anything on PostgreSQL, where transactions really overlap; SQLite runs them one at a time on its single connection.
func pilotRaces(t *testing.T, s *Store) {
	t.Helper()
	ctx := context.Background()
	id := "pilot-races-" + t.Name()
	t.Cleanup(func() { _, _ = s.db.Exec(s.q(`DELETE FROM playthroughs WHERE id = ?`), id) })
	if _, err := s.EnsurePlaythrough(ctx, id); err != nil {
		t.Fatal(err)
	}
	hangar := mekbay.Hangar{URL: goodHangarURL, Instance: goodInstance}
	living := func(camp string) (n int) {
		ps, err := s.Pilots(ctx, id, camp)
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if !p.Fallen {
				n++
			}
		}
		return n
	}
	const requests, rounds = 4, 15 // the store's PostgreSQL pool has 5 connections

	for round := 0; round < rounds; round++ {
		camp := fmt.Sprintf("race-%d", round)

		// Starting a campaign twice at once: one start, and the other is told it has been started (not a key violation).
		starting := []pilot.NewPilot{{Name: "Alrek"}, {Name: "Katarina"}, {Name: "Fallen", Portrait: "shared.png"}}
		oneWins(t, "start", atOnce(requests, func(int) error {
			return s.StartRunWithPilots(ctx, id, camp, hangar, starting, 5000, 1, "Test Force")
		}), ErrAlreadyStarted)
		ps, err := s.Pilots(ctx, id, camp)
		if err != nil || len(ps) != 3 {
			t.Fatalf("one start's pilots: %+v %v", ps, err)
		}
		var fallen pilot.NamedPilot
		for _, p := range ps {
			if p.Name == "Fallen" {
				fallen = p
			}
		}
		if err := s.MarkFallen(ctx, id, camp, fallen.ID); err != nil {
			t.Fatal(err)
		}

		// Two living pilots and room for three: of several recruits at once, one gets the place.
		oneWins(t, "recruit at the limit", atOnce(requests, func(i int) error {
			_, err := s.AddPilot(ctx, id, camp, fmt.Sprintf("Recruit %d", i), "", pilot.SPAllocation{}, 3)
			return err
		}), pilot.ErrPilotLimit)
		if n := living(camp); n != 3 {
			t.Fatalf("round %d: %d living pilots with a limit of 3", round, n)
		}

		// A recruit and a revive competing for the last place (the limit is now 4).
		errs := atOnce(2, func(i int) error {
			if i == 0 {
				return s.RevivePilot(ctx, id, camp, fallen.ID, "", 4)
			}
			_, err := s.AddPilot(ctx, id, camp, "Latecomer", "", pilot.SPAllocation{}, 4)
			return err
		})
		oneWins(t, "a recruit and a revive", errs, pilot.ErrPilotLimit)
		if n := living(camp); n != 4 {
			t.Fatalf("round %d: %d living pilots with a limit of 4", round, n)
		}

		// The same name, and the same portrait, asked for by several at once (the limit is out of the way).
		oneWins(t, "a name", atOnce(requests, func(int) error {
			_, err := s.AddPilot(ctx, id, camp, "Twin", "", pilot.SPAllocation{}, 100)
			return err
		}), ErrNameTaken)
		oneWins(t, "a portrait", atOnce(requests, func(i int) error {
			_, err := s.AddPilot(ctx, id, camp, fmt.Sprintf("Face %d", i), "face.png", pilot.SPAllocation{}, 100)
			return err
		}), pilot.ErrPortraitTaken)
		var two []string // the two starting pilots who are alive whoever won above
		for _, p := range ps {
			if p.ID != fallen.ID {
				two = append(two, p.ID)
			}
		}
		oneWins(t, "a rename", atOnce(2, func(i int) error { return s.RenamePilot(ctx, id, camp, two[i], "Namesake") }), ErrNameTaken)
		oneWins(t, "a portrait change", atOnce(2, func(i int) error { return s.SetPilotPortrait(ctx, id, camp, two[i], "new.png") }), pilot.ErrPortraitTaken)

		// A pilot with two Edge abilities to choose, chosen twice at once: one choice is kept.
		ace, err := s.AddPilot(ctx, id, camp, "Ace", "", pilot.SPAllocation{EdgeAbilities: 180}, 100)
		if err != nil {
			t.Fatal(err)
		}
		choices := [][]string{{"marksman", "nimble"}, {"patient", "bulwark"}, {"assassin", "cautious"}, {"protector", "speed_demon"}}
		oneWins(t, "Edge abilities", atOnce(requests, func(i int) error { return s.LearnAbilities(ctx, id, camp, ace.ID, choices[i]) }), ErrNoPendingAbilities)
		ps, _ = s.Pilots(ctx, id, camp)
		for _, p := range ps {
			if p.ID == ace.ID && (len(p.Learned) != 2 || p.PendingAbilities() != 0) {
				t.Fatalf("round %d: the pilot holds %v with a level of %d", round, p.Learned, p.EdgeAbilities())
			}
		}
	}
}

func TestPilotRacesSQLite(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	pilotRaces(t, s)
}

// TestPilotRacesPostgres runs only when TEST_DATABASE_URL points at a throwaway PostgreSQL database. It is the one that can fail: see pilotRaces.
func TestPilotRacesPostgres(t *testing.T) {
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	s, err := OpenPostgres(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	pilotRaces(t, s)
}
