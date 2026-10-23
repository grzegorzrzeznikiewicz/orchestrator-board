// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

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

func (s workflowHTTPService) Decide(ctx context.Context, id domain.SessionID, in localworkflow.DecisionRequest) (localworkflow.Snapshot, error) {
	if in.IntentID != "private-intent" || in.WorkflowID != "wf" || in.Kind != "accepted" || in.ExpectedVersion != 4 || in.SnapshotID != "snapshot" || in.TestOperationID != "tests" || in.ReviewRunID != "review" {
		return localworkflow.Snapshot{}, domain.ErrLocalWorkflowInvalid
	}
	return localworkflow.Snapshot{Accepted: true, Stage: "accepted", Decisions: []domain.LocalWorkflowDecision{{ID: "decision", WorkflowID: "wf", Kind: "accepted", Source: "desktop_user", SnapshotID: "snapshot"}}}, s.err
}

func TestLocalWorkflowDecisionHTTPContract(t *testing.T) {
	body := `{"requestId":"request","intentId":"private-intent","workflowId":"wf","expectedVersion":4,"kind":"accepted","comment":"checked","snapshotId":"snapshot","testOperationId":"tests","reviewRunId":"review"}`
	for _, tc := range []struct {
		name string
		err  error
		body string
		want int
		code string
	}{
		{"recorded", nil, body, 200, ""},
		{"denied", localworkflow.ErrDecisionDenied, body, 403, "LOCAL_WORKFLOW_DECISION_DENIED"},
		{"unavailable", localworkflow.ErrDecisionUnavailable, body, 503, "LOCAL_WORKFLOW_DECISION_UNAVAILABLE"},
		{"conflict", domain.ErrLocalWorkflowConflict, body, 409, "LOCAL_WORKFLOW_CONFLICT"},
		{"invalid", domain.ErrLocalWorkflowInvalid, body, 422, "LOCAL_WORKFLOW_INVALID"},
		{"unknown actor", nil, strings.TrimSuffix(body, "}") + `,"actor":"user"}`, 400, "INVALID_JSON"},
		{"trailing", nil, body + ` {}`, 400, "INVALID_JSON"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, httpd.APIDeps{LocalWorkflow: workflowHTTPService{err: tc.err}}, httpd.ControlDeps{})
			req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/worker/local-workflow/decision", strings.NewReader(tc.body))
			req.Header.Set("X-Request-ID", "decision-http")
			res := httptest.NewRecorder()
			router.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			if strings.Contains(res.Body.String(), "private-intent") {
				t.Fatal("private intent leaked")
			}
			if tc.want == 200 {
				if !strings.Contains(res.Body.String(), `"accepted":true`) || !strings.Contains(res.Body.String(), `"source":"desktop_user"`) {
					t.Fatalf("decision missing: %s", res.Body.String())
				}
			} else if !strings.Contains(res.Body.String(), tc.code) || !strings.Contains(res.Body.String(), "decision-http") {
				t.Fatalf("lost error envelope: %s", res.Body.String())
			}
		})
	}
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

type summaryHTTPService struct {
	workflowHTTPService
	ids *[]domain.SessionID
}

func TestLocalWorkflowScopedResumeRequiresCapableService(t *testing.T) {
	const scope = `{"workflowId":"wf","requestId":"scope-request","scopePath":"workflow-demo","basis":"Keep the original demo scope"}`
	for _, tc := range []struct {
		name, body string
		want       int
	}{
		{"ordinary resume", `{"expectedVersion":4}`, 200},
		{"null scope", `{"expectedVersion":4,"verificationScope":null}`, 200},
		{"unsupported scope never falls back", `{"expectedVersion":4,"verificationScope":` + scope + `}`, 501},
		{"invalid version", `{"expectedVersion":0,"verificationScope":` + scope + `}`, 422},
		{"caller cannot supply executable commands", `{"expectedVersion":4,"verificationScope":{"workflowId":"wf","requestId":"r","scopePath":"workflow-demo","basis":"demo","commands":["true"]}}`, 400},
		{"trailing input", `{"expectedVersion":4,"verificationScope":` + scope + `} {}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, httpd.APIDeps{LocalWorkflow: workflowHTTPService{}}, httpd.ControlDeps{})
			res := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/worker/local-workflow/resume", strings.NewReader(tc.body))
			req.Header.Set("X-Request-ID", "scoped-resume")
			router.ServeHTTP(res, req)
			if res.Code != tc.want {
				t.Fatalf("status=%d want=%d body=%s", res.Code, tc.want, res.Body.String())
			}
		})
	}
}

type scopedResumeHTTPService struct {
	workflowHTTPService
	calls *int
}

func (s scopedResumeHTTPService) Resume(context.Context, domain.SessionID, int64, *int) (localworkflow.Snapshot, error) {
	panic("scoped request must never use ordinary Resume")
}

func (s scopedResumeHTTPService) ResumeWithVerificationScope(ctx context.Context, id domain.SessionID, version int64, limit *int, scope *localworkflow.VerificationScopeRequest) (localworkflow.Snapshot, error) {
	*s.calls++
	if id != "worker" || version != 4 || limit == nil || *limit != 5 || scope.WorkflowID != "wf" || scope.RequestID != "scope-request" || scope.ScopePath != "workflow-demo" || scope.Basis != "Keep the original demo scope" {
		return localworkflow.Snapshot{}, domain.ErrLocalWorkflowInvalid
	}
	return s.Get(ctx, id)
}

func TestLocalWorkflowScopedResumeHTTPDispatch(t *testing.T) {
	const body = `{"expectedVersion":4,"maxRounds":5,"verificationScope":{"workflowId":"wf","requestId":"scope-request","scopePath":"workflow-demo","basis":"Keep the original demo scope"}}`
	for _, tc := range []struct {
		name, body, code string
		err              error
		want, calls      int
	}{
		{"scoped method", body, "", nil, 200, 1},
		{"stale contract", body, "LOCAL_WORKFLOW_CONFLICT", domain.ErrLocalWorkflowConflict, 409, 1},
		{"invalid contract", body, "LOCAL_WORKFLOW_INVALID", domain.ErrLocalWorkflowInvalid, 422, 1},
		{"invalid version before dispatch", strings.Replace(body, `"expectedVersion":4`, `"expectedVersion":0`, 1), "LOCAL_WORKFLOW_INVALID", nil, 422, 0},
		{"unknown nested field before dispatch", strings.Replace(body, `"scopePath":"workflow-demo"`, `"scopePath":"workflow-demo","actor":"user"`, 1), "INVALID_JSON", nil, 400, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls int
			svc := scopedResumeHTTPService{workflowHTTPService{err: tc.err}, &calls}
			router := httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, httpd.APIDeps{LocalWorkflow: svc}, httpd.ControlDeps{})
			res := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/sessions/worker/local-workflow/resume", strings.NewReader(tc.body))
			req.Header.Set("X-Request-ID", "scope-dispatch")
			router.ServeHTTP(res, req)
			if res.Code != tc.want || calls != tc.calls {
				t.Fatalf("status=%d calls=%d body=%s", res.Code, calls, res.Body.String())
			}
			if tc.code != "" && (!strings.Contains(res.Body.String(), tc.code) || !strings.Contains(res.Body.String(), "scope-dispatch")) {
				t.Fatalf("lost error envelope: %s", res.Body.String())
			}
		})
	}
}

func (s summaryHTTPService) Summaries(_ context.Context, ids []domain.SessionID) ([]localworkflow.CardSummary, error) {
	*s.ids = append([]domain.SessionID{}, ids...)
	return []localworkflow.CardSummary{{SessionID: "one", Stage: "testing", TestsPassed: 1, TestsTotal: 2}}, nil
}
func TestLocalWorkflowSummaryBatchHTTP(t *testing.T) {
	for _, tc := range []struct {
		name, query string
		want        int
	}{
		{"bounded batch", "one,two,one", 200},
		{"empty", "", 422},
		{"invalid", "one,%0A", 422},
		{"too many", strings.Repeat("one,", 200) + "two", 422},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var ids []domain.SessionID
			router := httpd.NewRouterWithControl(config.Config{}, slog.New(slog.NewTextHandler(io.Discard, nil)), nil, httpd.APIDeps{LocalWorkflow: summaryHTTPService{ids: &ids}}, httpd.ControlDeps{})
			res := httptest.NewRecorder()
			router.ServeHTTP(res, httptest.NewRequest(http.MethodGet, "/api/v1/local-workflow/summaries?sessionIds="+tc.query, nil))
			if res.Code != tc.want {
				t.Fatalf("status=%d body=%s", res.Code, res.Body.String())
			}
			if tc.want == 200 {
				if len(ids) != 2 || !strings.Contains(res.Body.String(), `"testsPassed":1`) {
					t.Fatalf("wrong batch response ids=%v body=%s", ids, res.Body.String())
				}
			} else if len(ids) > 0 {
				t.Fatal("invalid batch reached service")
			}
		})
	}
}
