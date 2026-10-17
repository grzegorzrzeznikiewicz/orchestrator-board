package controllers_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/config"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
)

type workflowHTTPService struct{ err error }

func (s workflowHTTPService) Get(context.Context, domain.SessionID) (localworkflow.Snapshot, error) {
	return localworkflow.Snapshot{Stage: "executing", Reviews: []domain.ReviewRun{{ID: "review-1", SnapshotID: "content-1", SnapshotPath: "/private/path", SnapshotObservation: "private-observation", Harness: domain.ReviewerClaudeCode, LocalResult: &domain.LocalReviewResult{Summary: "Checked task"}}}, Run: &domain.LocalWorkflowRun{ID: "wf", Task: "private task", Env: map[string]string{"SECRET": "do not expose"}, WorkspacePath: "/private/path", Version: 1, Phase: domain.WorkflowWaiting}}, s.err
}
func (s workflowHTTPService) Start(ctx context.Context, id domain.SessionID, _ string) (localworkflow.Snapshot, error) {
	return s.Get(ctx, id)
}
func (s workflowHTTPService) Stop(ctx context.Context, id domain.SessionID, _ int64) (localworkflow.Snapshot, error) {
	return s.Get(ctx, id)
}
func (s workflowHTTPService) Resume(ctx context.Context, id domain.SessionID, _ int64, _ *int) (localworkflow.Snapshot, error) {
	return s.Get(ctx, id)
}
func (s workflowHTTPService) TestLog(context.Context, domain.SessionID, string, string) (localworkflow.TestLog, error) {
	return localworkflow.TestLog{Text: "output"}, s.err
}
func TestLocalWorkflowHTTPContract(t *testing.T) {
	for _, tc := range []struct {
		name, method, body string
		svc                *workflowHTTPService
		want               int
	}{{"read", http.MethodGet, "", &workflowHTTPService{}, 200}, {"start", http.MethodPost, `{"requestId":"request"}`, &workflowHTTPService{}, 200}, {"trailing", http.MethodPost, `{"requestId":"request"} {}`, &workflowHTTPService{}, 400}, {"unknown field", http.MethodPost, `{"requestId":"request","operationId":"untrusted"}`, &workflowHTTPService{}, 400}, {"conflict", http.MethodPost, `{"requestId":"request"}`, &workflowHTTPService{domain.ErrLocalWorkflowConflict}, 409}, {"missing", http.MethodGet, "", &workflowHTTPService{domain.ErrLocalWorkflowNotFound}, 404}, {"invalid", http.MethodPost, `{"requestId":"request"}`, &workflowHTTPService{domain.ErrLocalWorkflowInvalid}, 422}, {"unavailable", http.MethodGet, "", nil, 501}} {
		t.Run(tc.name, func(t *testing.T) {
			deps := httpd.APIDeps{}
			if tc.svc != nil {
				deps.LocalWorkflow = tc.svc
			}
			router := httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, deps, httpd.ControlDeps{})
			path := "/api/v1/sessions/worker/local-workflow"
			if tc.method == http.MethodPost {
				path += "/start"
			}
			request := httptest.NewRequest(tc.method, path, strings.NewReader(tc.body))
			request.Header.Set("X-Request-ID", "wf-request")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != tc.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if strings.Contains(response.Body.String(), "SECRET") || strings.Contains(response.Body.String(), "/private/path") || strings.Contains(response.Body.String(), "private task") {
				t.Fatal("leaked private execution inputs")
			}
			if tc.want == 200 {
				var wire struct {
					Stage   string             `json:"stage"`
					Reviews []domain.ReviewRun `json:"reviews"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
					t.Fatal(err)
				}
				if wire.Stage != "executing" || len(wire.Reviews) != 1 || wire.Reviews[0].LocalResult.Summary != "Checked task" {
					t.Fatalf("lost workflow presentation: %s", response.Body.String())
				}
				if strings.Contains(response.Body.String(), "private-observation") {
					t.Fatal("leaked private observation")
				}
			}
			var result any
			if e := json.Unmarshal(response.Body.Bytes(), &result); e != nil {
				t.Fatal(e)
			}
		})
	}
}
