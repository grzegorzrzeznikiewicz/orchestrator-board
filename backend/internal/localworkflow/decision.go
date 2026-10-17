package localworkflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// Decision confirmation failures must never be treated as reviewer approval.
var (
	ErrDecisionDenied      = errors.New("local workflow decision was cancelled or denied")
	ErrDecisionUnavailable = errors.New("desktop decision confirmation is unavailable")
)

// DecisionRequest binds a user request to exact execution evidence. IntentID is
// private to the desktop-to-daemon transport, never a workflow response field.
type DecisionRequest struct {
	RequestID       string
	IntentID        string
	WorkflowID      string
	ExpectedVersion int64
	Kind            string
	Comment         string
	SnapshotID      string
	TestOperationID string
	ReviewRunID     string
}

// DecisionChallenge is confirmed only over the authenticated desktop channel.
type DecisionChallenge struct {
	Request   DecisionRequest
	SessionID domain.SessionID
	Cycle     int
}

// DecisionConfirmer requires a native, explicit desktop user confirmation.
type DecisionConfirmer interface {
	Confirm(context.Context, DecisionChallenge) error
}

func (in DecisionRequest) validate() error {
	for _, value := range []string{in.RequestID, in.IntentID, in.WorkflowID, in.SnapshotID, in.TestOperationID, in.ReviewRunID} {
		if strings.TrimSpace(value) == "" || len(value) > 256 || strings.ContainsAny(value, "\x00\r\n") {
			return domain.ErrLocalWorkflowInvalid
		}
	}
	if in.ExpectedVersion < 1 || (in.Kind != "accepted" && in.Kind != "changes_requested") || !utf8.ValidString(in.Comment) || utf8.RuneCountInString(in.Comment) > 4000 || strings.ContainsRune(in.Comment, 0) || in.Kind == "changes_requested" && strings.TrimSpace(in.Comment) == "" {
		return domain.ErrLocalWorkflowInvalid
	}
	return nil
}

func (in DecisionRequest) record(cycle int) domain.LocalWorkflowDecision {
	return domain.LocalWorkflowDecision{WorkflowID: in.WorkflowID, RequestID: in.RequestID, Kind: in.Kind, Comment: in.Comment, Source: "desktop_user", Cycle: cycle, ExpectedVersion: in.ExpectedVersion, SnapshotID: in.SnapshotID, TestOperationID: in.TestOperationID, ReviewRunID: in.ReviewRunID}
}

// Decide checks before and after confirmation, without holding workspace or run
// locks while a person reads the dialog. Exact retries are read-only.
func (c *Coordinator) Decide(ctx context.Context, id domain.SessionID, in DecisionRequest) (Snapshot, error) {
	if err := in.validate(); err != nil {
		return Snapshot{}, err
	}
	s, replayed, err := c.decisionCheckpoint(ctx, id, in, false)
	if err != nil || replayed {
		return s, err
	}
	if c.deps.Decisions == nil {
		return Snapshot{}, ErrDecisionUnavailable
	}
	confirmCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	if err := c.deps.Decisions.Confirm(confirmCtx, DecisionChallenge{Request: in, SessionID: id, Cycle: s.Run.Cycle}); err != nil {
		return Snapshot{}, err
	}
	if err = confirmCtx.Err(); err != nil {
		return Snapshot{}, fmt.Errorf("%w: confirmation expired", ErrDecisionDenied)
	}
	s, _, err = c.decisionCheckpoint(confirmCtx, id, in, true)
	return s, err
}

func (c *Coordinator) decisionCheckpoint(ctx context.Context, id domain.SessionID, in DecisionRequest, save bool) (Snapshot, bool, error) {
	unlock := c.lock(in.WorkflowID)
	defer unlock()
	r, found, err := c.deps.Store.GetLocalWorkflow(ctx, in.WorkflowID)
	if err != nil {
		return Snapshot{}, false, err
	}
	if !found || r.SessionID != id {
		return Snapshot{}, false, domain.ErrLocalWorkflowNotFound
	}
	previous, found, err := c.deps.Store.LocalWorkflowDecisionByRequest(ctx, r.ID, in.RequestID)
	if err != nil {
		return Snapshot{}, false, err
	}
	if found {
		if !previous.SameRequest(in.record(previous.Cycle)) {
			return Snapshot{}, false, domain.ErrLocalWorkflowConflict
		}
		s, err := c.Get(ctx, id)
		return s, true, err
	}
	release, err := c.deps.Store.AcquireLocalReviewWorkspace(ctx, id)
	if err != nil {
		return Snapshot{}, false, err
	}
	defer release()
	s, err := c.Get(ctx, id)
	if err != nil {
		return Snapshot{}, false, err
	}
	if s.Run == nil || s.Run.ID != in.WorkflowID || s.Run.Version != in.ExpectedVersion || s.Run.Phase != domain.WorkflowReady || s.Run.StopRequested || !s.Current || in.Kind == "accepted" && s.Accepted {
		return Snapshot{}, false, domain.ErrLocalWorkflowConflict
	}
	session, ok, err := c.deps.Store.GetSession(ctx, id)
	if err != nil {
		return Snapshot{}, false, err
	}
	if !ok || session.IsTerminated || session.Activity.State != domain.ActivityIdle {
		return Snapshot{}, false, domain.ErrLocalWorkflowConflict
	}
	project, ok, err := c.deps.Store.GetProject(ctx, string(session.ProjectID))
	if err != nil {
		return Snapshot{}, false, err
	}
	if !ok || project.Config.EffectiveReviewMode() != domain.ReviewTargetLocal {
		return Snapshot{}, false, domain.ErrLocalWorkflowConflict
	}
	pending, err := c.deps.Store.HasPendingLocalReviewWork(ctx, id)
	if err != nil {
		return Snapshot{}, false, err
	}
	obs, err := c.deps.Worker.Observe(ctx, id, "")
	if err != nil {
		return Snapshot{}, false, err
	}
	if pending || (obs.State != "idle" && obs.State != "completed") || obs.Generation != s.Run.WorkerGeneration {
		return Snapshot{}, false, domain.ErrLocalWorkflowConflict
	}
	test := latestOperation(s.Operations, "tests", s.Run.Cycle)
	review := latestOperation(s.Operations, "review", s.Run.Cycle)
	if test == nil || review == nil || test.ID != in.TestOperationID || review.ReviewRunID != in.ReviewRunID || test.SnapshotID != in.SnapshotID {
		return Snapshot{}, false, domain.ErrLocalWorkflowConflict
	}
	if !save {
		return s, false, nil
	}
	d := in.record(s.Run.Cycle)
	d.ID = uuid.NewString()
	d.CreatedAt = c.now()
	next := *s.Run
	next.UpdatedAt = d.CreatedAt
	if in.Kind == "changes_requested" {
		next.Phase = domain.WorkflowFixing
		if next.Cycle >= next.MaxRounds {
			next.Phase = domain.WorkflowBlocked
			next.BlockCode = "round_limit"
			next.BlockMessage = "User requested corrections at the round limit. Explicitly increase the limit to continue."
		}
	}
	changed, err := c.deps.Store.RecordLocalWorkflowDecision(ctx, next.Version, next, d)
	if err != nil {
		return Snapshot{}, false, err
	}
	if !changed {
		return Snapshot{}, false, domain.ErrLocalWorkflowConflict
	}
	if in.Kind == "changes_requested" {
		c.closeGuard(r.ID)
	}
	s, err = c.Get(ctx, id)
	return s, false, err
}

// currentEvidence validates the complete chain, not just an approval flag.
func currentEvidence(s Snapshot) bool {
	if s.Run == nil || s.Run.Phase != domain.WorkflowReady || s.Run.StopRequested || len(s.Run.TestCommands) == 0 {
		return false
	}
	test := latestOperation(s.Operations, "tests", s.Run.Cycle)
	review := latestOperation(s.Operations, "review", s.Run.Cycle)
	if test == nil || review == nil || test.State != "succeeded" || review.State != "succeeded" || test.CompletedAt == nil || review.CompletedAt == nil || review.InputOperationID != test.ID || test.SnapshotID == "" || review.SnapshotID != test.SnapshotID || review.ObservationID != test.ObservationID {
		return false
	}
	for _, op := range s.Operations {
		if activeOperation(op) {
			return false
		}
	}
	results := []domain.LocalWorkflowTestResult{}
	for _, result := range s.Tests {
		if result.OperationID == test.ID {
			results = append(results, result)
		}
	}
	if validateTestEvidence(*s.Run, *test, results) != nil {
		return false
	}
	for _, result := range s.Reviews {
		if result.ID == review.ReviewRunID {
			return result.Current && result.SnapshotID == test.SnapshotID && result.Verdict == domain.VerdictApproved && (result.Status == domain.ReviewRunComplete || result.Status == domain.ReviewRunDelivered) && result.LocalResult != nil && result.LocalResult.Outcome == "approved" && result.LocalResult.Validate(test.SnapshotID, result.Round) == nil
		}
	}
	return false
}

func acceptedEvidence(s Snapshot) bool {
	if !s.Current || len(s.Decisions) == 0 {
		return false
	}
	d := s.Decisions[len(s.Decisions)-1]
	test := latestOperation(s.Operations, "tests", s.Run.Cycle)
	review := latestOperation(s.Operations, "review", s.Run.Cycle)
	return d.Kind == "accepted" && d.Cycle == s.Run.Cycle && test != nil && review != nil && d.SnapshotID == test.SnapshotID && d.TestOperationID == test.ID && d.ReviewRunID == review.ReviewRunID
}

func (c *Coordinator) fixInput(ctx context.Context, r domain.LocalWorkflowRun, ops []domain.LocalWorkflowOperation) (string, string, error) {
	decisions, err := c.deps.Store.ListLocalWorkflowDecisions(ctx, r.ID)
	if err != nil {
		return "", "", err
	}
	if len(decisions) > 0 {
		d := decisions[len(decisions)-1]
		if d.Kind == "changes_requested" && d.Cycle == r.Cycle {
			return d.ID, BuildUserFixPrompt(r, d), nil
		}
	}
	review := latestOperation(ops, "review", r.Cycle)
	if review == nil {
		return "", "", fmt.Errorf("review result missing before corrections")
	}
	result, err := c.deps.Reviews.GetWorkflowLocal(ctx, review.ReviewRunID)
	if err != nil {
		return "", "", err
	}
	if result.LocalResult == nil {
		return "", "", fmt.Errorf("structured findings missing")
	}
	return review.ID, BuildFixPrompt(r, *result.LocalResult), nil
}
