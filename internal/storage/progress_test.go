package storage

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestScenarioCompletionRollsBackWhenOperationDeleteFails(t *testing.T) {
	s, err := OpenSQLite(filepath.Join(t.TempDir(), "progress.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := t.Context()
	const id, campaign, scenario = "test-progress", "refusalwar", "scenario_001"
	if _, err := s.EnsurePlaythrough(ctx, id); err != nil {
		t.Fatal(err)
	}
	op := MekBayOperation{OperationID: "test-operation"}
	if err := s.SaveMekBayOperation(ctx, id, campaign, scenario, op); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx, `CREATE TRIGGER reject_operation_delete BEFORE DELETE ON mekbay_operations
		BEGIN SELECT RAISE(ABORT, 'test operation delete failure'); END`); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.SetScenarioCompleted(ctx, id, campaign, scenario, true); changed || err == nil || !strings.Contains(err.Error(), "test operation delete failure") {
		t.Fatalf("completion got changed=%v, err=%v; want no change and injected delete failure", changed, err)
	}
	completed, err := s.Completions(ctx, id, campaign)
	if err != nil || completed[scenario] {
		t.Fatalf("failed completion must not persist: completions=%v, err=%v", completed, err)
	}
	got, ok, err := s.MekBayOperation(ctx, id, campaign, scenario)
	if err != nil || !ok || got != op {
		t.Fatalf("failed completion must preserve operation: got %+v, ok=%v, err=%v", got, ok, err)
	}
	if _, err := s.db.ExecContext(ctx, `DROP TRIGGER reject_operation_delete`); err != nil {
		t.Fatal(err)
	}
	if changed, err := s.SetScenarioCompleted(ctx, id, campaign, scenario, true); err != nil || !changed {
		t.Fatalf("retry after delete becomes possible: changed=%v, err=%v", changed, err)
	}
	if _, ok, err := s.MekBayOperation(ctx, id, campaign, scenario); err != nil || ok {
		t.Fatalf("successful completion must remove the operation: ok=%v, err=%v", ok, err)
	}
	if completed, err := s.Completions(ctx, id, campaign); err != nil || !completed[scenario] {
		t.Fatalf("successful completion must persist: completions=%v, err=%v", completed, err)
	}

}
