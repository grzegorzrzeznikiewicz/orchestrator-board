// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func startLocalWorkflow(ctx context.Context, store *sqlite.Store, manager sessionLifecycle, chat localworkflow.ChatInterrupter, reviews localworkflow.Reviews, decisions localworkflow.DecisionConfirmer, dataDir string, log *slog.Logger) (*localworkflow.Coordinator, error) {
	delivery, ok := manager.(localworkflow.WorkerDelivery)
	if !ok {
		return nil, fmt.Errorf("session manager does not support durable workflow delivery")
	}
	planner, ok := manager.(interface {
		RunBackgroundTask(context.Context, domain.SessionID, string, string) (string, error)
	})
	if !ok {
		return nil, fmt.Errorf("session manager does not support verification planning")
	}
	coordinator := localworkflow.New(localworkflow.Deps{Planner: localworkflow.ModelPlanner{Generate: planner.RunBackgroundTask}, Decisions: decisions, Store: store, Worker: localworkflow.NewSessionWorker(store, delivery, chat), Tests: localworkflow.NewTestRunner(dataDir), Reviews: reviews, DataDir: dataDir, Logger: log})
	if e := coordinator.Recover(ctx); e != nil {
		_ = coordinator.Close(context.Background())
		return nil, e
	}
	go coordinator.Run(ctx)
	return coordinator, nil
}
