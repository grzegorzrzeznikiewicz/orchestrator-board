package daemon

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite"
)

func startLocalWorkflow(ctx context.Context, store *sqlite.Store, manager sessionLifecycle, chat localworkflow.ChatInterrupter, reviews localworkflow.Reviews, dataDir string, log *slog.Logger) (*localworkflow.Coordinator, error) {
	delivery, ok := manager.(localworkflow.WorkerDelivery)
	if !ok {
		return nil, fmt.Errorf("session manager does not support durable workflow delivery")
	}
	coordinator := localworkflow.New(localworkflow.Deps{Store: store, Worker: localworkflow.NewSessionWorker(store, delivery, chat), Tests: localworkflow.NewTestRunner(dataDir), Reviews: reviews, DataDir: dataDir, Logger: log})
	if e := coordinator.Recover(ctx); e != nil {
		_ = coordinator.Close(context.Background())
		return nil, e
	}
	go coordinator.Run(ctx)
	return coordinator, nil
}
