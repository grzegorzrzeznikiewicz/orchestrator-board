// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/sqlitetest"
)

func TestWorkflowDecisionAtomicVersionConflict(t *testing.T) {
	s, r, d, _ := decisionStoreFixture(t)
	ctx := context.Background()
	d.ExpectedVersion = 2
	if ok, err := s.RecordLocalWorkflowDecision(ctx, 2, r, d); err != nil || ok {
		t.Fatalf("stale write: %v %v", ok, err)
	}
	decisions, err := s.ListLocalWorkflowDecisions(ctx, r.ID)
	if err != nil || len(decisions) != 0 {
		t.Fatalf("partial decision: %+v %v", decisions, err)
	}
	d.ExpectedVersion = 1
	d.WorkflowID = "another-workflow"
	if _, err = s.RecordLocalWorkflowDecision(ctx, 1, r, d); err == nil {
		t.Fatal("foreign decision allowed")
	}
	saved, _, err := s.GetLocalWorkflow(ctx, r.ID)
	if err != nil || saved.Version != 1 {
		t.Fatalf("partial version: %+v %v", saved, err)
	}
}

func TestWorkflowDecisionRetryIdentity(t *testing.T) {
	s, r, d, _ := decisionStoreFixture(t)
	ctx := context.Background()
	before, err := s.LatestSeq(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := s.RecordLocalWorkflowDecision(ctx, 1, r, d); err != nil || !ok {
		t.Fatalf("record: %v %v", ok, err)
	}
	if ok, err := s.RecordLocalWorkflowDecision(ctx, 1, r, d); err != nil || ok {
		t.Fatalf("retry: %v %v", ok, err)
	}
	d.Comment = "different"
	if _, err := s.RecordLocalWorkflowDecision(ctx, 1, r, d); !errors.Is(err, domain.ErrLocalWorkflowConflict) {
		t.Fatalf("changed request: %v", err)
	}
	decisions, err := s.ListLocalWorkflowDecisions(ctx, r.ID)
	saved, _, getErr := s.GetLocalWorkflow(ctx, r.ID)
	if err != nil || getErr != nil || len(decisions) != 1 || saved.Version != 2 {
		t.Fatalf("retry changed history: %+v %+v %v %v", decisions, saved, err, getErr)
	}
	events, err := s.EventsAfter(ctx, before, 100)
	if err != nil || len(events) != 1 || events[0].Type != "session_updated" {
		t.Fatalf("decision CDC/retry: %+v %v", events, err)
	}
	d.RequestID = "new-request"
	d.ExpectedVersion = 2
	if _, err := s.RecordLocalWorkflowDecision(ctx, 2, saved, d); err == nil {
		t.Fatal("duplicate decision ID allowed")
	}
	rolledBack, _, err := s.GetLocalWorkflow(ctx, r.ID)
	if err != nil || rolledBack.Version != 2 {
		t.Fatalf("failed insert advanced checkpoint: %+v %v", rolledBack, err)
	}
}

func TestWorkflowDecisionSurvivesReopen(t *testing.T) {
	s, r, d, dir := decisionStoreFixture(t)
	ctx := context.Background()
	if ok, err := s.RecordLocalWorkflowDecision(ctx, 1, r, d); err != nil || !ok {
		t.Fatalf("record: %v %v", ok, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	got, found, err := reopened.LocalWorkflowDecisionByRequest(ctx, r.ID, d.RequestID)
	if err != nil || !found || got.Comment != "checked by user" || got.SnapshotID != "snapshot" || got.TestOperationID != "tests" || got.ReviewRunID != "review" || got.Source != "desktop_user" || !got.CreatedAt.Equal(d.CreatedAt) {
		t.Fatalf("reopen: %+v %v %v", got, found, err)
	}
}

func decisionStoreFixture(t *testing.T) (*sqlite.Store, domain.LocalWorkflowRun, domain.LocalWorkflowDecision, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := sqlitetest.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ctx := context.Background()
	if err = s.UpsertProject(ctx, domain.ProjectRecord{ID: "decisions", Path: "/fixture", RegisteredAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	w, err := s.CreateSession(ctx, sampleRecord("decisions"))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	r := domain.LocalWorkflowRun{ID: "decision-flow", SessionID: w.ID, Phase: domain.WorkflowReady, Cycle: 1, MaxRounds: 3, CreatedAt: now, UpdatedAt: now}
	r, _, err = s.CreateLocalWorkflow(ctx, r, "start")
	if err != nil {
		t.Fatal(err)
	}
	d := domain.LocalWorkflowDecision{ID: "decision", WorkflowID: r.ID, RequestID: "request", Kind: "accepted", Comment: "checked by user", Source: "desktop_user", Cycle: 1, ExpectedVersion: 1, SnapshotID: "snapshot", TestOperationID: "tests", ReviewRunID: "review", CreatedAt: now}
	return s, r, d, dir
}
