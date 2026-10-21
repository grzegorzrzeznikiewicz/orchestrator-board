// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package controllers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
)

type providerRetryHTTPService struct {
	workflowHTTPService
	retry *domain.LocalWorkflowProviderRetry
}

func (s providerRetryHTTPService) Get(context.Context, domain.SessionID) (localworkflow.Snapshot, error) {
	return localworkflow.Snapshot{Run: &domain.LocalWorkflowRun{ID: "wf", Phase: domain.WorkflowBlocked, ProviderRetry: s.retry}, PreviousRuns: []domain.LocalWorkflowRun{{ID: "prior", ProviderRetry: s.retry}}}, nil
}

func (s providerRetryHTTPService) Summaries(context.Context, []domain.SessionID) ([]localworkflow.CardSummary, error) {
	return []localworkflow.CardSummary{{SessionID: "worker", Run: &localworkflow.CardRun{ID: "wf", Phase: domain.WorkflowBlocked, ProviderRetry: s.retry}}}, nil
}

func TestProviderRetryFullHistoryAndSummaryHTTPContract(t *testing.T) {
	next := time.Date(2026, 10, 20, 10, 2, 0, 0, time.UTC)
	retry := &domain.LocalWorkflowProviderRetry{Stage: "review", State: "waiting", Reason: domain.ProviderAccessAuthentication, Attempt: 2, MaxAttempts: 3, FailureAt: next.Add(-time.Minute), LastAt: &next, NextAt: &next, OperationID: "failed-review", AttemptID: "reserved", ContextID: strings.Repeat("a", 64)}
	router := httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, httpd.APIDeps{LocalWorkflow: providerRetryHTTPService{retry: retry}}, httpd.ControlDeps{})
	for _, path := range []string{"/api/v1/sessions/worker/local-workflow", "/api/v1/local-workflow/summaries?sessionIds=worker"} {
		t.Run(path, func(t *testing.T) {
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, path, nil))
			if res.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			var body struct {
				Run          domain.LocalWorkflowRun   `json:"run"`
				PreviousRuns []domain.LocalWorkflowRun `json:"previousRuns"`
				Summaries    []struct {
					Run domain.LocalWorkflowRun `json:"run"`
				} `json:"summaries"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			runs := append([]domain.LocalWorkflowRun{body.Run}, body.PreviousRuns...)
			if len(body.Summaries) > 0 {
				runs = []domain.LocalWorkflowRun{body.Summaries[0].Run}
			}
			for _, run := range runs {
				if !reflect.DeepEqual(run.ProviderRetry, retry) {
					t.Fatalf("lost retry facts: %+v", run.ProviderRetry)
				}
			}
		})
	}
}
