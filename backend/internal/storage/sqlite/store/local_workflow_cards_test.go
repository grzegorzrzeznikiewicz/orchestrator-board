// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestWorkflowCardsBatchExcludesEvidenceAndIncludesEmptySessions(t *testing.T) {
	s := newTestStore(t)
	seedProject(t, s, "cards")
	worker, err := s.CreateSession(t.Context(), sampleRecord("cards"))
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.CreateSession(t.Context(), sampleRecord("cards"))
	if err != nil {
		t.Fatal(err)
	}
	run := domain.LocalWorkflowRun{ID: "card-run", SessionID: worker.ID, Phase: domain.WorkflowWaiting, Cycle: 1, MaxRounds: 3, Automatic: true, StopRequested: true,
		Task: "PRIVATE TASK", Env: map[string]string{"SECRET": "PRIVATE VALUE"}, ExecutionOutput: "PRIVATE OUTPUT", WorkspacePath: "/PRIVATE/WORKSPACE",
		TestCommands: []string{"secret-command"}, VerificationPlan: &domain.LocalVerificationPlan{Profile: "implementation", Rationale: "PRIVATE RATIONALE", Checks: []string{"PRIVATE CHECK"}}, CreatedAt: histClock, UpdatedAt: histClock}
	if _, _, err := s.CreateLocalWorkflow(t.Context(), run, "card-request"); err != nil {
		t.Fatal(err)
	}
	reader, ok := any(s).(interface {
		ListLocalWorkflowCardFacts(context.Context, []domain.SessionID) ([]domain.LocalWorkflowCardFacts, error)
	})
	if !ok {
		t.Fatal("Kanban still needs full workflow history for every card")
	}
	facts, err := reader.ListLocalWorkflowCardFacts(t.Context(), []domain.SessionID{empty.ID, worker.ID, "missing"})
	if err != nil || len(facts) != 2 {
		t.Fatalf("batch projection: %+v %v", facts, err)
	}
	if facts[0].SessionID != empty.ID || facts[0].Run != nil || facts[1].SessionID != worker.ID || facts[1].Run == nil || facts[1].Run.ID != run.ID || facts[1].TestsTotal != 1 || !facts[1].Run.Automatic || !facts[1].Run.StopRequested {
		t.Fatalf("incorrect card facts: %+v", facts)
	}
	b, err := json.Marshal(facts)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"PRIVATE", "secret-command"} {
		if strings.Contains(string(b), secret) {
			t.Fatalf("display projection loads private evidence: %s", secret)
		}
	}
	if _, err := reader.ListLocalWorkflowCardFacts(t.Context(), make([]domain.SessionID, 201)); err == nil {
		t.Fatal("unbounded batch accepted")
	}
}

func TestWorkflowCardsDurableReviewValidityAndBoundedFindings(t *testing.T) {
	s := newTestStore(t)
	ctx := t.Context()
	seedProject(t, s, "card-review")
	rec := sampleRecord("card-review")
	rec.Activity.State = domain.ActivityIdle
	worker, err := s.CreateSession(ctx, rec)
	if err != nil {
		t.Fatal(err)
	}
	run, _, err := s.CreateLocalWorkflow(ctx, domain.LocalWorkflowRun{ID: "card-review-run", SessionID: worker.ID, Phase: domain.WorkflowReviewing, Cycle: 1, MaxRounds: 3, CreatedAt: histClock, UpdatedAt: histClock}, "review-request")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertReview(ctx, domain.Review{ID: "card-reviewer", SessionID: worker.ID, ProjectID: worker.ProjectID, Harness: domain.ReviewerClaudeCode, CreatedAt: histClock, UpdatedAt: histClock}); err != nil {
		t.Fatal(err)
	}
	operation := domain.LocalWorkflowOperation{ID: "card-review-op", WorkflowID: run.ID, Kind: "review", Cycle: 1, Attempt: 1, State: "succeeded", ReviewRunID: "card-review-result"}
	if updated, err := s.AdvanceLocalWorkflow(ctx, run.Version, run, &operation); err != nil || !updated {
		t.Fatalf("operation: %v %v", updated, err)
	}
	review := domain.ReviewRun{ID: operation.ReviewRunID, ReviewID: "card-reviewer", SessionID: worker.ID, TargetKind: domain.ReviewTargetLocal, Harness: domain.ReviewerClaudeCode, SnapshotID: "card-snapshot", SnapshotPath: "/snapshot/card", Status: domain.ReviewRunRunning, CreatedAt: histClock}
	if err := s.InsertReviewRun(ctx, review); err != nil {
		t.Fatal(err)
	}
	result := &domain.LocalReviewResult{Version: 1, SnapshotID: review.SnapshotID, Round: 1, Outcome: "changes_requested", Summary: "Required change", Findings: []domain.LocalReviewFinding{{Kind: "required", Path: "file.go", Line: 1, Reason: strings.Repeat("ą", 2000)}, {Kind: "suggestion", Path: "file.go", Line: 2, Reason: "optional"}}}
	if updated, err := s.UpdateLocalReviewResult(ctx, review.ID, domain.ReviewRunComplete, domain.VerdictChangesRequested, result.Markdown(), result); err != nil || !updated {
		t.Fatalf("result: %v %v", updated, err)
	}
	facts, err := s.ListLocalWorkflowCardFacts(ctx, []domain.SessionID{worker.ID, worker.ID})
	if err != nil || len(facts) != 1 {
		t.Fatalf("facts %v %v", facts, err)
	}
	fact := facts[0]
	if !fact.ExecutorIdle || fact.ReviewRunID != review.ID || fact.ReviewValid || fact.RequiredCount != 1 || len([]rune(fact.RequiredReason)) != 1024 {
		t.Fatalf("incorrect durable projection: %+v", fact)
	}
	// A later approved review is current only while its durable validity and
	// executor state hold, even if the filesystem and workflow version stand still.
	review.ID = "card-review-approved"
	if err := s.InsertReviewRun(ctx, review); err != nil {
		t.Fatal(err)
	}
	result.Outcome = "approved"
	result.Findings = []domain.LocalReviewFinding{}
	if updated, err := s.UpdateLocalReviewResult(ctx, review.ID, domain.ReviewRunComplete, domain.VerdictApproved, "approved", result); err != nil || !updated {
		t.Fatalf("approve %v %v", updated, err)
	}
	run, _, err = s.GetLocalWorkflow(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	operation.ID = "card-review-op-2"
	operation.Attempt = 2
	operation.ReviewRunID = review.ID
	if updated, err := s.AdvanceLocalWorkflow(ctx, run.Version, run, &operation); err != nil || !updated {
		t.Fatalf("next op %v %v", updated, err)
	}
	facts, err = s.ListLocalWorkflowCardFacts(ctx, []domain.SessionID{worker.ID})
	if err != nil || !facts[0].ReviewValid || facts[0].RequiredCount != 0 {
		t.Fatalf("approved facts %+v %v", facts, err)
	}
	if err := s.InvalidateLocalReviewRun(ctx, review.ID, "new executor work", histClock); err != nil {
		t.Fatal(err)
	}
	facts, err = s.ListLocalWorkflowCardFacts(ctx, []domain.SessionID{worker.ID})
	if err != nil || facts[0].ReviewValid {
		t.Fatalf("invalidated facts %+v %v", facts, err)
	}
	worker.Activity.State = domain.ActivityActive
	if err := s.UpdateSession(ctx, worker); err != nil {
		t.Fatal(err)
	}
	facts, err = s.ListLocalWorkflowCardFacts(ctx, []domain.SessionID{worker.ID})
	if err != nil || facts[0].ExecutorIdle {
		t.Fatalf("busy facts %+v %v", facts, err)
	}
}
