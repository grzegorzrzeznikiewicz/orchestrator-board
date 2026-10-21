// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package localworkflow

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestProviderRecoveryStalePlannerCompletionSettlesBeforeFreshVerification(t *testing.T) {
	const command = `test "$(cat value)" = correct`
	f := setupWorkflow(t, []string{command}, 3)
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	released := false
	t.Cleanup(func() {
		if !released {
			close(release)
		}
	})
	var calls atomic.Int32
	f.coordinator.deps.Planner = plannerFunc(func(_ context.Context, run domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		rationale := "Fresh planner result"
		if calls.Add(1) == 1 {
			close(entered)
			<-release
			rationale = "Stale planner result"
		}
		return domain.LocalVerificationPlan{Profile: "implementation", Rationale: rationale, Commands: append([]string{}, run.TestCommands...), Checks: []string{"Current code passes its configured regression"}}, nil
	})
	s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "stale-provider-completion", "Implement and verify")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("initial planner did not start")
	}
	f.worker.mu.Lock()
	f.worker.generation = "chat:replacement"
	f.worker.mu.Unlock()
	if err = f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
		t.Fatal(err)
	}
	blocked, err := f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if blocked.Run.BlockCode != "worker_changed" || blocked.Run.PlanState != "running" {
		t.Fatalf("worker change did not fence the in-flight planner: %+v", blocked.Run)
	}
	close(release)
	released = true
	awaitProviderPlannerExit(t, f.coordinator, s.Run.ID)
	settled, err := f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settled.Run.PlanState != "canceled" || settled.Run.Phase != domain.WorkflowBlocked || settled.Run.BlockCode != "worker_changed" || settled.Run.VerificationPlan != nil || settled.Run.ProviderRetry != nil || len(settled.Operations) != 0 || settled.Current || settled.Accepted {
		t.Fatalf("known stale completion was lost or applied: run=%+v operations=%+v", settled.Run, settled.Operations)
	}
	ready := driveAutomaticReady(t, f, s.Run.ID)
	tests, review := latestOperation(ready.Operations, "tests", ready.Run.Cycle), latestOperation(ready.Operations, "review", ready.Run.Cycle)
	if calls.Load() != 2 || ready.Run.VerificationPlan == nil || ready.Run.VerificationPlan.Rationale != "Fresh planner result" || ready.Run.WorkerGeneration != "chat:replacement" || tests == nil || tests.State != "succeeded" || review == nil || review.InputOperationID != tests.ID || len(ready.Tests) != 1 || ready.Tests[0].Status != "succeeded" || len(ready.Reviews) != 1 || !ready.Current || ready.Accepted || len(ready.Decisions) != 0 {
		t.Fatalf("fresh planning, tests, review or human gate missing: calls=%d run=%+v operations=%+v", calls.Load(), ready.Run, ready.Operations)
	}
}
