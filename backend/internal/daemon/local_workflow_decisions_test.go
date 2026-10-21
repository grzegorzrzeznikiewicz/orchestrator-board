// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package daemon

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/browserruntime"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
)

type decisionBrokerFunc func(context.Context, domain.SessionID, string, map[string]interface{}) (browserruntime.Result, error)

func (f decisionBrokerFunc) Execute(ctx context.Context, id domain.SessionID, action string, args map[string]interface{}) (browserruntime.Result, error) {
	return f(ctx, id, action, args)
}

func TestLocalWorkflowDecisionConfirmerRejectsMissingNativeEvidence(t *testing.T) {
	for _, value := range []any{nil, false, map[string]any{"confirmed": false}, map[string]any{"confirmed": "true"}, map[string]any{"confirmed": true, "unexpected": true}} {
		confirmer := newLocalWorkflowDecisionConfirmer(decisionBrokerFunc(func(context.Context, domain.SessionID, string, map[string]interface{}) (browserruntime.Result, error) {
			return browserruntime.Result{Value: value}, nil
		}))
		if err := confirmer.Confirm(context.Background(), localworkflow.DecisionChallenge{}); !errors.Is(err, localworkflow.ErrDecisionDenied) {
			t.Fatalf("accepted invalid result %#v: %v", value, err)
		}
	}
}

func TestLocalWorkflowDecisionConfirmerBindsExactIntent(t *testing.T) {
	c := newLocalWorkflowDecisionConfirmer(decisionBrokerFunc(func(_ context.Context, id domain.SessionID, action string, args map[string]interface{}) (browserruntime.Result, error) {
		want := map[string]interface{}{"sessionId": "session", "cycle": 2, "requestId": "request", "intentId": "private", "workflowId": "flow", "expectedVersion": int64(5), "kind": "accepted", "comment": "checked", "snapshotId": "snapshot", "testOperationId": "tests", "reviewRunId": "review"}
		if id != "session" || action != "__confirm-local-workflow-decision" || !reflect.DeepEqual(args, want) {
			t.Fatalf("wrong challenge: %s %s %#v", id, action, args)
		}
		return browserruntime.Result{Value: map[string]any{"confirmed": true}}, nil
	}))
	in := localworkflow.DecisionRequest{RequestID: "request", IntentID: "private", WorkflowID: "flow", ExpectedVersion: 5, Kind: "accepted", Comment: "checked", SnapshotID: "snapshot", TestOperationID: "tests", ReviewRunID: "review"}
	if err := c.Confirm(context.Background(), localworkflow.DecisionChallenge{Request: in, SessionID: "session", Cycle: 2}); err != nil {
		t.Fatal(err)
	}
}

func TestLocalWorkflowDecisionConfirmerBrokerFailures(t *testing.T) {
	for _, tc := range []struct {
		failure error
		want    error
	}{
		{browserruntime.CommandError{Code: "LOCAL_WORKFLOW_DECISION_DENIED"}, localworkflow.ErrDecisionDenied},
		{context.DeadlineExceeded, localworkflow.ErrDecisionUnavailable},
		{errors.New("desktop disconnected"), localworkflow.ErrDecisionUnavailable},
	} {
		c := newLocalWorkflowDecisionConfirmer(decisionBrokerFunc(func(context.Context, domain.SessionID, string, map[string]interface{}) (browserruntime.Result, error) {
			return browserruntime.Result{}, tc.failure
		}))
		if err := c.Confirm(context.Background(), localworkflow.DecisionChallenge{}); !errors.Is(err, tc.want) {
			t.Fatalf("failure %v mapped to %v", tc.failure, err)
		}
	}
	if err := newLocalWorkflowDecisionConfirmer(nil).Confirm(context.Background(), localworkflow.DecisionChallenge{}); !errors.Is(err, localworkflow.ErrDecisionUnavailable) {
		t.Fatalf("missing broker: %v", err)
	}
}
