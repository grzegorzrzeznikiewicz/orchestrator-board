// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package localworkflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type confirmDecisionFunc func(context.Context, DecisionChallenge) error

func (f confirmDecisionFunc) Confirm(ctx context.Context, d DecisionChallenge) error {
	return f(ctx, d)
}

func readyHumanDecision(t *testing.T, limit int) (*workflowFixture, Snapshot, DecisionRequest) {
	t.Helper()
	f := setupWorkflow(t, []string{"test -f value"}, limit)
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.coordinator.Start(context.Background(), f.session.ID, "start"); err != nil {
		t.Fatal(err)
	}
	s := awaitPhase(t, f, domain.WorkflowReady)
	test := latestOperation(s.Operations, "tests", s.Run.Cycle)
	review := latestOperation(s.Operations, "review", s.Run.Cycle)
	return f, s, DecisionRequest{RequestID: "human-request", IntentID: "private-intent", WorkflowID: s.Run.ID, ExpectedVersion: s.Run.Version, Kind: "accepted", Comment: "checked", SnapshotID: test.SnapshotID, TestOperationID: test.ID, ReviewRunID: review.ReviewRunID}
}

func TestWorkflowHumanAcceptanceRequiresCurrentEvidence(t *testing.T) {
	for _, name := range []string{"no confirmation", "denied", "snapshot", "test", "review", "version", "required finding", "executor", "terminated", "files", "stopped"} {
		t.Run(name, func(t *testing.T) {
			f, s, in := readyHumanDecision(t, 3)
			confirmed := 0
			f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { confirmed++; return nil })
			switch name {
			case "no confirmation":
				f.coordinator.deps.Decisions = nil
			case "denied":
				f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { return ErrDecisionDenied })
			case "snapshot":
				in.SnapshotID = "foreign"
			case "test":
				in.TestOperationID = "foreign"
			case "review":
				in.ReviewRunID = "foreign"
			case "version":
				in.ExpectedVersion++
			case "required finding":
				f.reviews.mu.Lock()
				r := f.reviews.runs[in.ReviewRunID]
				r.LocalResult.Findings = append(r.LocalResult.Findings, domain.LocalReviewFinding{Kind: "required", Path: "value", Line: 1, Reason: "must fix"})
				f.reviews.runs[r.ID] = r
				f.reviews.mu.Unlock()
			case "executor":
				f.worker.mu.Lock()
				f.worker.stopped = true
				f.worker.mu.Unlock()
			case "terminated":
				r := f.session
				r.IsTerminated = true
				if err := f.store.UpdateSession(context.Background(), r); err != nil {
					t.Fatal(err)
				}
			case "files":
				if err := os.WriteFile(filepath.Join(f.root, "new-file"), []byte("new"), 0600); err != nil {
					t.Fatal(err)
				}
			case "stopped":
				if _, err := f.coordinator.Stop(context.Background(), f.session.ID, s.Run.Version); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := f.coordinator.Decide(context.Background(), f.session.ID, in); err == nil {
				t.Fatal("invalid acceptance succeeded")
			}
			after, err := f.coordinator.Get(context.Background(), f.session.ID)
			if err != nil || after.Accepted || len(after.Decisions) != 0 {
				t.Fatalf("invalid acceptance persisted: %+v %v", after, err)
			}
			if confirmed != 0 {
				t.Fatal("invalid evidence reached confirmation")
			}
		})
	}
}

func TestWorkflowHumanDecisionRechecksAfterConfirmation(t *testing.T) {
	for _, name := range []string{"files", "stop"} {
		t.Run(name, func(t *testing.T) {
			f, s, in := readyHumanDecision(t, 3)
			f.coordinator.deps.Decisions = confirmDecisionFunc(func(_ context.Context, d DecisionChallenge) error {
				if d.Request.Kind != "accepted" || d.SessionID != f.session.ID {
					t.Fatal("wrong challenge")
				}
				if name == "files" {
					return os.WriteFile(filepath.Join(f.root, "value"), []byte("changed"), 0600)
				}
				_, err := f.coordinator.Stop(context.Background(), f.session.ID, s.Run.Version)
				return err
			})
			if _, err := f.coordinator.Decide(context.Background(), f.session.ID, in); !errors.Is(err, domain.ErrLocalWorkflowConflict) {
				t.Fatalf("changed while confirming: %v", err)
			}
			after, err := f.coordinator.Get(context.Background(), f.session.ID)
			if err != nil || len(after.Decisions) != 0 || after.Accepted {
				t.Fatalf("stale accepted: %+v %v", after, err)
			}
		})
	}
}

func TestWorkflowHumanDecisionRetry(t *testing.T) {
	f, _, in := readyHumanDecision(t, 3)
	confirmed := 0
	f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { confirmed++; return nil })
	for range 2 {
		s, err := f.coordinator.Decide(context.Background(), f.session.ID, in)
		if err != nil || !s.Accepted || s.Stage != "accepted" || len(s.Decisions) != 1 {
			t.Fatalf("decision: %+v %v", s, err)
		}
	}
	if confirmed != 1 {
		t.Fatalf("confirmed %d times", confirmed)
	}
	in.Comment = "different"
	if _, err := f.coordinator.Decide(context.Background(), f.session.ID, in); !errors.Is(err, domain.ErrLocalWorkflowConflict) {
		t.Fatalf("changed retry: %v", err)
	}
}

func TestWorkflowUserCorrectionSource(t *testing.T) {
	f, s, in := readyHumanDecision(t, 1)
	f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { return nil })
	accepted, err := f.coordinator.Decide(context.Background(), f.session.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	in.RequestID = "return-request"
	in.ExpectedVersion = accepted.Run.Version
	in.Kind = "changes_requested"
	in.Comment = "also explain the fix"
	blocked, err := f.coordinator.Decide(context.Background(), f.session.ID, in)
	if err != nil || blocked.Accepted || blocked.Run.BlockCode != "round_limit" || len(blocked.Decisions) != 2 {
		t.Fatalf("return at limit: %+v %v", blocked, err)
	}
	if f.worker.fixes != 0 {
		t.Fatal("sent corrections past limit")
	}
	limit := 2
	if _, err = f.coordinator.Resume(context.Background(), f.session.ID, blocked.Run.Version, &limit); err != nil {
		t.Fatal(err)
	}
	done := awaitPhase(t, f, domain.WorkflowReady)
	if done.Run.Cycle != 2 || done.Accepted || len(done.Decisions) != 2 {
		t.Fatalf("verification not renewed: %+v", done)
	}
	op := latestOperation(done.Operations, "fix", s.Run.Cycle)
	if op == nil || op.InputOperationID != done.Decisions[1].ID {
		t.Fatalf("wrong correction input: %+v", op)
	}
	if !strings.Contains(BuildUserFixPrompt(*s.Run, done.Decisions[1]), "also explain the fix") {
		t.Fatal("user comment missing")
	}
	for range 3 {
		if err = f.coordinator.Evaluate(context.Background(), done.Run.ID); err != nil {
			t.Fatal(err)
		}
	}
	if f.worker.fixes != 1 {
		t.Fatalf("corrections duplicated: %d", f.worker.fixes)
	}
}

func TestWorkflowDecisionInvalidationAndRecovery(t *testing.T) {
	f, _, in := readyHumanDecision(t, 3)
	f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { return nil })
	if _, err := f.coordinator.Decide(context.Background(), f.session.ID, in); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := f.coordinator.Get(context.Background(), f.session.ID)
	if err != nil || s.Accepted || len(s.Decisions) != 1 {
		t.Fatalf("revived acceptance: %+v %v", s, err)
	}
	if err = f.coordinator.Recover(context.Background()); err != nil {
		t.Fatal(err)
	}
	s, err = f.coordinator.Get(context.Background(), f.session.ID)
	if err != nil || s.Accepted || len(s.Decisions) != 1 || s.Run.BlockCode != "restart" {
		t.Fatalf("recovery: %+v %v", s, err)
	}
}

func TestWorkflowNewUserCorrectionAfterCompletedFix(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 4)
	if _, err := f.coordinator.Start(context.Background(), f.session.ID, "start"); err != nil {
		t.Fatal(err)
	}
	s := awaitPhase(t, f, domain.WorkflowReady)
	if f.worker.fixes != 1 || s.Run.Cycle != 2 {
		t.Fatalf("first reviewer correction missing: %+v", s.Run)
	}
	test := latestOperation(s.Operations, "tests", s.Run.Cycle)
	review := latestOperation(s.Operations, "review", s.Run.Cycle)
	in := DecisionRequest{RequestID: "new-user-correction", IntentID: "private", WorkflowID: s.Run.ID, ExpectedVersion: s.Run.Version, Kind: "changes_requested", Comment: "Please explain the corrected behavior", SnapshotID: test.SnapshotID, TestOperationID: test.ID, ReviewRunID: review.ReviewRunID}
	f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { return nil })
	if _, err := f.coordinator.Decide(context.Background(), f.session.ID, in); err != nil {
		t.Fatal(err)
	}
	done := awaitPhase(t, f, domain.WorkflowReady)
	for range 3 {
		if err := f.coordinator.Evaluate(context.Background(), done.Run.ID); err != nil {
			t.Fatal(err)
		}
	}
	op := latestOperation(done.Operations, "fix", 2)
	if f.worker.fixes != 2 || done.Run.Cycle != 3 || op == nil || op.InputOperationID != done.Decisions[0].ID {
		t.Fatalf("new user correction not separately delivered: fixes=%d run=%+v op=%+v", f.worker.fixes, done.Run, op)
	}
}

func TestWorkflowPendingUserCorrectionSurvivesRecovery(t *testing.T) {
	for _, failure := range []string{"restart", "worker_changed"} {
		t.Run(failure, func(t *testing.T) {
			f, s, in := readyHumanDecision(t, 3)
			f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { return nil })
			in.Kind = "changes_requested"
			in.Comment = "Explain the exact correction after recovery"
			if _, err := f.coordinator.Decide(context.Background(), f.session.ID, in); err != nil {
				t.Fatal(err)
			}
			if failure == "restart" {
				if err := f.coordinator.Recover(context.Background()); err != nil {
					t.Fatal(err)
				}
			} else {
				f.worker.mu.Lock()
				f.worker.generation = "chat:replacement"
				f.worker.mu.Unlock()
				if err := f.coordinator.Evaluate(context.Background(), s.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			blocked, err := f.coordinator.Get(context.Background(), f.session.ID)
			if err != nil || blocked.Run.BlockCode != failure {
				t.Fatalf("expected %s: %+v %v", failure, blocked.Run, err)
			}
			if _, err = f.coordinator.Resume(context.Background(), f.session.ID, blocked.Run.Version, nil); err != nil {
				t.Fatal(err)
			}
			done := awaitPhase(t, f, domain.WorkflowReady)
			f.worker.mu.Lock()
			fixes := f.worker.fixes
			prompts := append([]string{}, f.worker.prompts...)
			f.worker.mu.Unlock()
			if fixes != 1 || done.Run.Cycle != 2 || len(prompts) != 1 || !strings.Contains(prompts[0], in.Comment) || !strings.Contains(prompts[0], s.Run.Task) {
				t.Fatalf("pending user correction lost: fixes=%d cycle=%d prompts=%q", fixes, done.Run.Cycle, prompts)
			}
			replay, err := f.coordinator.Decide(context.Background(), f.session.ID, in)
			if err != nil || replay.Accepted || len(replay.Decisions) != 1 {
				t.Fatalf("old decision replay: %+v %v", replay, err)
			}
			for range 2 {
				if err = f.coordinator.Evaluate(context.Background(), done.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			f.worker.mu.Lock()
			defer f.worker.mu.Unlock()
			if f.worker.fixes != 1 {
				t.Fatalf("duplicate correction: %d", f.worker.fixes)
			}
		})
	}
}
