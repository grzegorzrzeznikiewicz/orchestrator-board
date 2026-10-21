// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/storage/sqlite/gen"
)

// Private execution inputs are persisted separately from the public JSON view.
type workflowRecord struct {
	ExecutionOutput         string
	VerificationAfterTurnID string
	ReviewerConfig          domain.AgentConfig
	domain.LocalWorkflowRun
	Request      string
	TaskText     string
	LatestPrompt string
	Environment  map[string]string
	Workspace    string
	Generation   string
}

func checkLocalWorkflowWorkerAdmission(ctx context.Context, q *gen.Queries, id domain.SessionID) error {
	blocked, e := q.LocalWorkflowBlocksWorker(ctx, string(id))
	if e != nil {
		return e
	}
	if blocked {
		return fmt.Errorf("%w: tests or review own the workspace; stop the workflow before sending new work", domain.ErrLocalWorkflowConflict)
	}
	return nil
}

// CheckLocalWorkflowWorkerAdmission rejects worker intake while verification owns the workspace.
func (s *Store) CheckLocalWorkflowWorkerAdmission(ctx context.Context, id domain.SessionID) error {
	return checkLocalWorkflowWorkerAdmission(ctx, s.qr, id)
}

func workflowNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

func encodeWorkflow(r domain.LocalWorkflowRun) (string, error) {
	b, e := json.Marshal(workflowRecord{ExecutionOutput: r.ExecutionOutput, VerificationAfterTurnID: r.VerificationAfterTurnID, ReviewerConfig: r.ReviewerConfig, LocalWorkflowRun: r, Request: r.RequestID, TaskText: r.Task, LatestPrompt: r.LatestUserPrompt, Environment: r.Env, Workspace: r.WorkspacePath, Generation: r.WorkerGeneration})
	return string(b), e
}
func decodeWorkflow(row gen.LocalWorkflow) (domain.LocalWorkflowRun, error) {
	var p workflowRecord
	if e := json.Unmarshal([]byte(row.RecordJson), &p); e != nil {
		return domain.LocalWorkflowRun{}, e
	}
	r := p.LocalWorkflowRun
	r.ExecutionOutput = p.ExecutionOutput
	r.VerificationAfterTurnID = p.VerificationAfterTurnID
	r.ReviewerConfig = p.ReviewerConfig
	r.RequestID = p.Request
	r.Task = p.TaskText
	r.LatestUserPrompt = p.LatestPrompt
	r.Env = p.Environment
	r.WorkspacePath = p.Workspace
	r.WorkerGeneration = p.Generation
	return r, nil
}

type workflowOperationRecord struct {
	domain.LocalWorkflowOperation
	WorkerGeneration string
	DeliveryID       string
	Observation      string
}

func encodeWorkflowOperation(r domain.LocalWorkflowOperation) (string, error) {
	b, e := json.Marshal(workflowOperationRecord{r, r.Generation, r.ClientMessageID, r.ObservationID})
	return string(b), e
}

type workflowTestRecord struct {
	domain.LocalWorkflowTestResult
	OutPath string
	ErrPath string
}

// CreateLocalWorkflow atomically creates one open run per session or returns its retry.
func (s *Store) CreateLocalWorkflow(ctx context.Context, r domain.LocalWorkflowRun, requestID string) (domain.LocalWorkflowRun, bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	old, e := s.qw.LocalWorkflowByRequest(ctx, gen.LocalWorkflowByRequestParams{SessionID: string(r.SessionID), RequestID: requestID})
	if e == nil {
		previous, e := decodeWorkflow(old)
		if e != nil {
			return r, false, e
		}
		if previous.Task != r.Task || previous.LatestUserPrompt != r.LatestUserPrompt || previous.MaxRounds != r.MaxRounds || previous.WorkspacePath != r.WorkspacePath || !reflect.DeepEqual(previous.TestCommands, r.TestCommands) || !reflect.DeepEqual(previous.Env, r.Env) {
			return r, false, fmt.Errorf("%w: request id already has different execution inputs", domain.ErrLocalWorkflowConflict)
		}
		return previous, false, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return r, false, e
	}
	current, e := s.qw.CurrentLocalWorkflow(ctx, string(r.SessionID))
	if e == nil && !current.ClosedAt.Valid {
		return r, false, fmt.Errorf("%w: session already has an open workflow", domain.ErrLocalWorkflowConflict)
	}
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return r, false, e
	}
	r.RequestID = requestID
	r.Version = 1
	raw, e := encodeWorkflow(r)
	if e != nil {
		return r, false, e
	}
	e = s.qw.InsertLocalWorkflow(ctx, gen.InsertLocalWorkflowParams{ID: r.ID, SessionID: string(r.SessionID), RequestID: requestID, Version: 1, Phase: string(r.Phase), ClosedAt: workflowNullTime(r.ClosedAt), RecordJson: raw})
	return r, e == nil, e
}

// GetLocalWorkflow reads a persisted run by its durable identity.
func (s *Store) GetLocalWorkflow(ctx context.Context, id string) (domain.LocalWorkflowRun, bool, error) {
	row, e := s.qr.GetLocalWorkflow(ctx, id)
	if errors.Is(e, sql.ErrNoRows) {
		return domain.LocalWorkflowRun{}, false, nil
	}
	if e != nil {
		return domain.LocalWorkflowRun{}, false, e
	}
	r, e := decodeWorkflow(row)
	return r, e == nil, e
}

// CurrentLocalWorkflow reads the open run or latest historical run for a session.
func (s *Store) CurrentLocalWorkflow(ctx context.Context, id domain.SessionID) (domain.LocalWorkflowRun, bool, error) {
	row, e := s.qr.CurrentLocalWorkflow(ctx, string(id))
	if errors.Is(e, sql.ErrNoRows) {
		return domain.LocalWorkflowRun{}, false, nil
	}
	if e != nil {
		return domain.LocalWorkflowRun{}, false, e
	}
	r, e := decodeWorkflow(row)
	return r, e == nil, e
}

// ListOpenLocalWorkflows returns runs that still own a session checkpoint.
func (s *Store) ListOpenLocalWorkflows(ctx context.Context) ([]domain.LocalWorkflowRun, error) {
	rows, e := s.qr.ListOpenLocalWorkflows(ctx)
	if e != nil {
		return nil, e
	}
	out := make([]domain.LocalWorkflowRun, 0, len(rows))
	for _, row := range rows {
		r, e := decodeWorkflow(row)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, nil
}

// AdvanceLocalWorkflow atomically publishes a versioned checkpoint and optional reservation.
// cannot write tests, complete an operation, or publish a new phase.
func (s *Store) AdvanceLocalWorkflow(ctx context.Context, version int64, r domain.LocalWorkflowRun, op *domain.LocalWorkflowOperation) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	changed := false
	e := s.inTx(ctx, "advance local workflow", func(q *gen.Queries) error {
		ok, e := advanceWorkflow(ctx, q, version, r)
		if e != nil || !ok {
			return e
		}
		changed = true
		if op == nil {
			return nil
		}
		if op.WorkflowID != r.ID {
			return fmt.Errorf("operation belongs to another workflow")
		}
		raw, e := encodeWorkflowOperation(*op)
		if e != nil {
			return e
		}
		return q.InsertLocalWorkflowOperation(ctx, gen.InsertLocalWorkflowOperationParams{ID: op.ID, WorkflowID: op.WorkflowID, Cycle: int64(op.Cycle), Kind: op.Kind, Attempt: int64(op.Attempt), State: op.State, ClientMessageID: op.ClientMessageID, RecordJson: raw})
	})
	return changed && e == nil, e
}
func advanceWorkflow(ctx context.Context, q *gen.Queries, version int64, r domain.LocalWorkflowRun) (bool, error) {
	r.Version = version + 1
	raw, e := encodeWorkflow(r)
	if e != nil {
		return false, e
	}
	n, e := q.AdvanceLocalWorkflow(ctx, gen.AdvanceLocalWorkflowParams{NextVersion: r.Version, Phase: string(r.Phase), ClosedAt: workflowNullTime(r.ClosedAt), RecordJson: raw, ID: r.ID, ExpectedVersion: version})
	return n == 1, e
}

// CompleteLocalWorkflowOperation atomically settles an operation, its tests and the next checkpoint.
func (s *Store) CompleteLocalWorkflowOperation(ctx context.Context, version int64, op domain.LocalWorkflowOperation, r domain.LocalWorkflowRun, tests []domain.LocalWorkflowTestResult) (bool, error) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	changed := false
	e := s.inTx(ctx, "complete local workflow operation", func(q *gen.Queries) error {
		if op.WorkflowID != r.ID {
			return fmt.Errorf("operation belongs to another workflow")
		}
		ok, e := advanceWorkflow(ctx, q, version, r)
		if e != nil || !ok {
			return e
		}
		raw, e := encodeWorkflowOperation(op)
		if e != nil {
			return e
		}
		n, e := q.CompleteLocalWorkflowOperation(ctx, gen.CompleteLocalWorkflowOperationParams{ID: op.ID, WorkflowID: r.ID, State: op.State, RecordJson: raw})
		if e != nil {
			return e
		}
		if n != 1 {
			return fmt.Errorf("%w: operation already completed", domain.ErrLocalWorkflowConflict)
		}
		for _, test := range tests {
			if test.OperationID != op.ID {
				return fmt.Errorf("test belongs to another operation")
			}
			b, e := json.Marshal(workflowTestRecord{test, test.StdoutLog, test.StderrLog})
			if e != nil {
				return e
			}
			if e := q.InsertLocalWorkflowTest(ctx, gen.InsertLocalWorkflowTestParams{ID: test.ID, OperationID: op.ID, CommandIndex: int64(test.CommandIndex), RecordJson: string(b)}); e != nil {
				return e
			}
		}
		changed = true
		return nil
	})
	return changed && e == nil, e
}

// ListLocalWorkflowOperations reads the ordered side-effect history of a run.
func (s *Store) ListLocalWorkflowOperations(ctx context.Context, id string) ([]domain.LocalWorkflowOperation, error) {
	rows, e := s.qr.ListLocalWorkflowOperations(ctx, id)
	if e != nil {
		return nil, e
	}
	out := make([]domain.LocalWorkflowOperation, 0, len(rows))
	for _, row := range rows {
		var p workflowOperationRecord
		if e := json.Unmarshal([]byte(row.RecordJson), &p); e != nil {
			return nil, e
		}
		op := p.LocalWorkflowOperation
		op.Generation = p.WorkerGeneration
		op.ClientMessageID = p.DeliveryID
		op.ObservationID = p.Observation
		out = append(out, op)
	}
	return out, nil
}

// ListLocalWorkflowTests reads command results in operation and command order.
func (s *Store) ListLocalWorkflowTests(ctx context.Context, id string) ([]domain.LocalWorkflowTestResult, error) {
	rows, e := s.qr.ListLocalWorkflowTests(ctx, id)
	if e != nil {
		return nil, e
	}
	out := make([]domain.LocalWorkflowTestResult, 0, len(rows))
	for _, row := range rows {
		var p workflowTestRecord
		if e := json.Unmarshal([]byte(row.RecordJson), &p); e != nil {
			return nil, e
		}
		r := p.LocalWorkflowTestResult
		r.StdoutLog = p.OutPath
		r.StderrLog = p.ErrPath
		out = append(out, r)
	}
	return out, nil
}

// GetWorkflowReviewRun finds the review created for an identified workflow operation.
func (s *Store) GetWorkflowReviewRun(ctx context.Context, operationID string) (domain.ReviewRun, bool, error) {
	id, e := s.qr.ReviewByWorkflowOperation(ctx, operationID)
	if errors.Is(e, sql.ErrNoRows) {
		return domain.ReviewRun{}, false, nil
	}
	if e != nil {
		return domain.ReviewRun{}, false, e
	}
	return s.GetReviewRun(ctx, id)
}

// CheckLocalWorkflowReviewAdmission rejects review launches not owned by the active checkpoint.
func (s *Store) CheckLocalWorkflowReviewAdmission(ctx context.Context, id domain.SessionID, operationID string) error {
	r, ok, e := s.CurrentLocalWorkflow(ctx, id)
	if e != nil || !ok || r.ClosedAt != nil {
		return e
	}
	if r.Phase == domain.WorkflowBlocked || r.Phase == domain.WorkflowStopped || r.Phase == domain.WorkflowReady {
		return nil
	}
	if operationID != "" && r.Phase == domain.WorkflowReviewing && !r.StopRequested {
		ops, e := s.ListLocalWorkflowOperations(ctx, r.ID)
		if e != nil {
			return e
		}
		for _, op := range ops {
			if op.ID == operationID && op.Kind == "review" && op.State == "reserved" {
				return nil
			}
		}
	}
	return fmt.Errorf("%w: active local workflow owns review admission", domain.ErrLocalWorkflowConflict)
}

// A controller replacement between workflow preflight and Chat intake cannot
// accept a reserved fix intended for the previous provider generation.
func checkLocalWorkflowFixGeneration(ctx context.Context, q *gen.Queries, id domain.SessionID, deliveryID, generation string) error {
	if deliveryID == "" {
		return nil
	}
	row, err := q.CurrentLocalWorkflow(ctx, string(id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if row.ClosedAt.Valid {
		return nil
	}
	ops, err := q.ListLocalWorkflowOperations(ctx, row.ID)
	if err != nil {
		return err
	}
	for _, op := range ops {
		if op.ClientMessageID == deliveryID && op.Kind == "fix" {
			var record workflowOperationRecord
			if err := json.Unmarshal([]byte(op.RecordJson), &record); err != nil {
				return err
			}
			if record.WorkerGeneration != "chat:"+generation {
				return fmt.Errorf("%w: correction belongs to a different executor controller", domain.ErrLocalWorkflowConflict)
			}
			if row.Phase != "fixing" || op.State != "reserved" && op.State != "running" {
				return fmt.Errorf("%w: correction is no longer admitted", domain.ErrLocalWorkflowConflict)
			}
		}
	}
	return nil
}
