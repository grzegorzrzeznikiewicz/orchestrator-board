package daemon

import (
	"context"
	"errors"

	"github.com/aoagents/agent-orchestrator/backend/internal/browserruntime"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
)

type localWorkflowDecisionBroker interface {
	Execute(context.Context, domain.SessionID, string, map[string]interface{}) (browserruntime.Result, error)
}
type localWorkflowDecisionConfirmer struct{ broker localWorkflowDecisionBroker }

func newLocalWorkflowDecisionConfirmer(broker localWorkflowDecisionBroker) localworkflow.DecisionConfirmer {
	return localWorkflowDecisionConfirmer{broker}
}

func (c localWorkflowDecisionConfirmer) Confirm(ctx context.Context, challenge localworkflow.DecisionChallenge) error {
	if c.broker == nil {
		return localworkflow.ErrDecisionUnavailable
	}
	in := challenge.Request
	result, err := c.broker.Execute(ctx, challenge.SessionID, "__confirm-local-workflow-decision", map[string]interface{}{"sessionId": string(challenge.SessionID), "cycle": challenge.Cycle, "requestId": in.RequestID, "intentId": in.IntentID, "workflowId": in.WorkflowID, "expectedVersion": in.ExpectedVersion, "kind": in.Kind, "comment": in.Comment, "snapshotId": in.SnapshotID, "testOperationId": in.TestOperationID, "reviewRunId": in.ReviewRunID})
	if err != nil {
		var command browserruntime.CommandError
		if errors.As(err, &command) && command.Code == "LOCAL_WORKFLOW_DECISION_DENIED" {
			return localworkflow.ErrDecisionDenied
		}
		return localworkflow.ErrDecisionUnavailable
	}
	value, ok := result.Value.(map[string]interface{})
	if !ok || len(value) != 1 || value["confirmed"] != true {
		return localworkflow.ErrDecisionDenied
	}
	return nil
}
