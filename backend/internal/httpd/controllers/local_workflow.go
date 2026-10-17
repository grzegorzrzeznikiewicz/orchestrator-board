package controllers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/apispec"
	"github.com/aoagents/agent-orchestrator/backend/internal/httpd/envelope"
	"github.com/aoagents/agent-orchestrator/backend/internal/localworkflow"
)

// LocalWorkflowService is the HTTP-facing local workflow command and read boundary.
type LocalWorkflowService interface {
	Get(context.Context, domain.SessionID) (localworkflow.Snapshot, error)
	Start(context.Context, domain.SessionID, string) (localworkflow.Snapshot, error)
	Stop(context.Context, domain.SessionID, int64) (localworkflow.Snapshot, error)
	Resume(context.Context, domain.SessionID, int64, *int) (localworkflow.Snapshot, error)
	TestLog(context.Context, domain.SessionID, string, string) (localworkflow.TestLog, error)
}

// LocalWorkflowController mounts explicit local workflow control routes.
type LocalWorkflowController struct{ Svc LocalWorkflowService }

// Register mounts workflow routes even when the service is unavailable.
func (c *LocalWorkflowController) Register(r chi.Router) {
	r.Get("/sessions/{sessionId}/local-workflow", c.get)
	r.Post("/sessions/{sessionId}/local-workflow/start", c.start)
	r.Post("/sessions/{sessionId}/local-workflow/stop", c.stop)
	r.Post("/sessions/{sessionId}/local-workflow/resume", c.resume)
	r.Get("/sessions/{sessionId}/local-workflow/tests/{testId}/logs", c.log)
}
func (c *LocalWorkflowController) available(w http.ResponseWriter, r *http.Request, suffix string) bool {
	if c.Svc != nil {
		return true
	}
	apispec.NotImplemented(w, r, r.Method, "/api/v1/sessions/{sessionId}/local-workflow"+suffix)
	return false
}
func localWorkflowResponse(w http.ResponseWriter, r *http.Request, result localworkflow.Snapshot, err error) {
	if err == nil {
		envelope.WriteJSON(w, http.StatusOK, LocalWorkflowResponse{Run: result.Run, Operations: result.Operations, Tests: result.Tests, Current: result.Current, RequiredFindings: result.RequiredFindings, Stage: result.Stage, Reviews: result.Reviews})
		return
	}
	status := http.StatusInternalServerError
	kind := "internal"
	code := "LOCAL_WORKFLOW_FAILED"
	message := "Local workflow operation failed"
	switch {
	case errors.Is(err, domain.ErrLocalWorkflowConflict):
		status = 409
		kind = "conflict"
		code = "LOCAL_WORKFLOW_CONFLICT"
		message = err.Error()
	case errors.Is(err, domain.ErrLocalWorkflowInvalid):
		status = 422
		kind = "unprocessable"
		code = "LOCAL_WORKFLOW_INVALID"
		message = err.Error()
	case errors.Is(err, domain.ErrLocalWorkflowNotFound):
		status = 404
		kind = "not_found"
		code = "LOCAL_WORKFLOW_NOT_FOUND"
		message = err.Error()
	}
	envelope.WriteAPIError(w, r, status, kind, code, message, nil)
}
func strictWorkflowJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	decoder.DisallowUnknownFields()
	err := decoder.Decode(out)
	if err == nil {
		var extra any
		if e := decoder.Decode(&extra); e != io.EOF {
			err = errors.New("expected a single JSON object")
		}
	}
	if err != nil {
		envelope.WriteAPIError(w, r, 400, "bad_request", "INVALID_JSON", "Expected one valid JSON object with known fields", nil)
		return false
	}
	return true
}
func (c *LocalWorkflowController) get(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r, "") {
		return
	}
	result, e := c.Svc.Get(r.Context(), sessionID(r))
	localWorkflowResponse(w, r, result, e)
}
func (c *LocalWorkflowController) start(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r, "/start") {
		return
	}
	var in StartLocalWorkflowRequest
	if !strictWorkflowJSON(w, r, &in) {
		return
	}
	result, e := c.Svc.Start(r.Context(), sessionID(r), in.RequestID)
	localWorkflowResponse(w, r, result, e)
}
func (c *LocalWorkflowController) stop(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r, "/stop") {
		return
	}
	var in StopLocalWorkflowRequest
	if !strictWorkflowJSON(w, r, &in) {
		return
	}
	if in.ExpectedVersion < 1 {
		localWorkflowResponse(w, r, localworkflow.Snapshot{}, domain.ErrLocalWorkflowInvalid)
		return
	}
	result, e := c.Svc.Stop(r.Context(), sessionID(r), in.ExpectedVersion)
	localWorkflowResponse(w, r, result, e)
}
func (c *LocalWorkflowController) resume(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r, "/resume") {
		return
	}
	var in ResumeLocalWorkflowRequest
	if !strictWorkflowJSON(w, r, &in) {
		return
	}
	if in.ExpectedVersion < 1 {
		localWorkflowResponse(w, r, localworkflow.Snapshot{}, domain.ErrLocalWorkflowInvalid)
		return
	}
	result, e := c.Svc.Resume(r.Context(), sessionID(r), in.ExpectedVersion, in.MaxRounds)
	localWorkflowResponse(w, r, result, e)
}
func (c *LocalWorkflowController) log(w http.ResponseWriter, r *http.Request) {
	if !c.available(w, r, "/tests/{testId}/logs") {
		return
	}
	result, e := c.Svc.TestLog(r.Context(), sessionID(r), chi.URLParam(r, "testId"), r.URL.Query().Get("stream"))
	if e != nil {
		localWorkflowResponse(w, r, localworkflow.Snapshot{}, e)
		return
	}
	envelope.WriteJSON(w, 200, LocalWorkflowTestLogResponse{result.Text, result.Truncated})
}
