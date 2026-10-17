package localworkflow

import (
	"context"
	"sync"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func TestWorkflowStopAndResumeDoNotResetCycle(t *testing.T) {
	f := setupWorkflow(t, []string{"sleep 10"}, 3)
	ctx := context.Background()
	start, e := f.coordinator.Start(ctx, f.session.ID, "start")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.coordinator.Evaluate(ctx, start.Run.ID); e != nil {
		t.Fatal(e)
	}
	running, e := f.coordinator.Get(ctx, f.session.ID)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = f.coordinator.Stop(ctx, f.session.ID, running.Run.Version); e != nil {
		t.Fatal(e)
	}
	stopped := awaitPhase(t, f, domain.WorkflowStopped)
	if stopped.Run.Cycle != 1 || stopped.Current {
		t.Fatalf("stopped=%+v", stopped.Run)
	}
	resumed, e := f.coordinator.Resume(ctx, f.session.ID, stopped.Run.Version, nil)
	if e != nil {
		t.Fatal(e)
	}
	if resumed.Run.Cycle != 1 || resumed.Run.MaxRounds != 3 || resumed.Run.StopRequested {
		t.Fatalf("resume=%+v", resumed.Run)
	}
	if _, e = f.coordinator.Resume(ctx, f.session.ID, stopped.Run.Version, nil); e == nil {
		t.Fatal("stale resume accepted")
	}
}
func TestWorkflowRestartAndDuplicateEvaluation(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	ctx := context.Background()
	start, e := f.coordinator.Start(ctx, f.session.ID, "start")
	if e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for range 5 {
		wg.Go(func() {
			if e := f.coordinator.Evaluate(ctx, start.Run.ID); e != nil {
				t.Error(e)
			}
		})
	}
	wg.Wait()
	ready := awaitPhase(t, f, domain.WorkflowReady)
	if f.worker.fixes != 1 || len(ready.Tests) != 2 {
		t.Fatal("duplicate side effect")
	}
	if e = f.coordinator.Close(ctx); e != nil {
		t.Fatal(e)
	}
	if e = f.store.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, e := sqlite.Open(f.data)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = reopened.Close() }()
	other := New(Deps{Store: reopened, Worker: f.worker, Tests: NewTestRunner(f.data), Reviews: f.reviews, DataDir: f.data})
	defer func() { _ = other.Close(ctx) }()
	if e = other.Recover(ctx); e != nil {
		t.Fatal(e)
	}
	restored, e := other.Get(ctx, f.session.ID)
	assertWorkflowPresentation(t, restored, "local_review", 2)
	if e != nil || restored.Current || restored.Run.Phase != domain.WorkflowBlocked || len(restored.Tests) != 2 {
		t.Fatalf("restored=%+v %v", restored, e)
	}
}
func TestWorkflowLimitRequiresExplicitIncrease(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 1)
	f.reviews.alwaysReject = true
	ctx := context.Background()
	if _, e := f.coordinator.Start(ctx, f.session.ID, "start"); e != nil {
		t.Fatal(e)
	}
	blocked := awaitPhase(t, f, domain.WorkflowBlocked)
	if _, e := f.coordinator.Resume(ctx, f.session.ID, blocked.Run.Version, nil); e == nil {
		t.Fatal("limit bypassed by resume")
	}
	limit := 2
	resumed, e := f.coordinator.Resume(ctx, f.session.ID, blocked.Run.Version, &limit)
	if e != nil {
		t.Fatal(e)
	}
	if resumed.Run.MaxRounds != 2 || resumed.Run.Cycle != 1 || resumed.Run.Phase != domain.WorkflowFixing {
		t.Fatalf("resume limit=%+v", resumed.Run)
	}
}
