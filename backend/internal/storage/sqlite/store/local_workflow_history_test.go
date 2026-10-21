// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func TestLocalWorkflowHistoryKeepsPreviousTaskScopes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	seedProject(t, s, "history")
	worker, err := s.CreateSession(ctx, sampleRecord("history"))
	if err != nil {
		t.Fatal(err)
	}
	closed := histClock.Add(time.Minute)
	for _, run := range []domain.LocalWorkflowRun{
		{ID: "previous", SessionID: worker.ID, Phase: domain.WorkflowReady, Task: "original accepted task", Cycle: 1, MaxRounds: 3, CreatedAt: histClock, UpdatedAt: closed, ClosedAt: &closed},
		{ID: "current", SessionID: worker.ID, Phase: domain.WorkflowWaiting, Task: "new user follow-up", Cycle: 1, MaxRounds: 3, CreatedAt: closed, UpdatedAt: closed},
	} {
		if _, _, err := s.CreateLocalWorkflow(ctx, run, run.ID+"-request"); err != nil {
			t.Fatal(err)
		}
	}
	history, ok := any(s).(interface {
		ListSessionLocalWorkflows(context.Context, domain.SessionID) ([]domain.LocalWorkflowRun, error)
	})
	if !ok {
		t.Fatal("store cannot retrieve previous workflow scopes")
	}
	runs, err := history.ListSessionLocalWorkflows(ctx, worker.ID)
	if err != nil || len(runs) != 2 || runs[0].ID != "current" || runs[0].Task != "new user follow-up" || runs[1].ID != "previous" || runs[1].Task != "original accepted task" {
		t.Fatalf("wrong scoped history: %+v %v", runs, err)
	}
}
