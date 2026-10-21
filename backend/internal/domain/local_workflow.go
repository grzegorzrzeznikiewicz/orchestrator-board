// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package domain

import (
	"errors"
	"time"
)

// Local workflow errors distinguish conflicts, invalid inputs and missing records.
var (
	ErrLocalWorkflowConflict = errors.New("local workflow conflict")
	ErrLocalWorkflowInvalid  = errors.New("invalid local workflow")
	ErrLocalWorkflowNotFound = errors.New("local workflow not found")
)

// LocalWorkflowRun records execution facts, not the session's display status.
// Private configuration is deliberately excluded from public JSON responses.
type LocalWorkflowRun struct {
	ProviderRetry           *LocalWorkflowProviderRetry `json:"providerRetry,omitempty"`
	Automatic               bool                        `json:"automatic,omitempty"`
	PlanState               string                      `json:"planState,omitempty"`
	VerificationPlan        *LocalVerificationPlan      `json:"verificationPlan,omitempty"`
	ExecutionOutput         string                      `json:"-"`
	VerificationAfterTurnID string                      `json:"-"`
	ReviewerConfig          AgentConfig                 `json:"-"`
	ID                      string                      `json:"id"`
	SessionID               SessionID                   `json:"sessionId"`
	RequestID               string                      `json:"-"`
	Version                 int64                       `json:"version"`
	Phase                   LocalWorkflowPhase          `json:"phase" enum:"waiting_executor,testing,reviewing,fixing,ready,blocked,stopping,stopped"`
	Cycle                   int                         `json:"cycle"`
	MaxRounds               int                         `json:"maxRounds"`
	Task                    string                      `json:"-"`
	LatestUserPrompt        string                      `json:"-"`
	TestCommands            []string                    `json:"testCommands"`
	Env                     map[string]string           `json:"-"`
	WorkspacePath           string                      `json:"-"`
	WorkerGeneration        string                      `json:"-"`
	StopRequested           bool                        `json:"stopRequested"`
	BlockCode               string                      `json:"blockCode,omitempty"`
	BlockMessage            string                      `json:"blockMessage,omitempty"`
	CreatedAt               time.Time                   `json:"createdAt"`
	UpdatedAt               time.Time                   `json:"updatedAt"`
	ClosedAt                *time.Time                  `json:"closedAt,omitempty"`
}

// LocalWorkflowPhase identifies the durable execution checkpoint of a local workflow.
type LocalWorkflowPhase string

// LocalWorkflowDecision is an immutable, evidence-bound desktop user decision.
// Current acceptance is derived separately; old decisions remain in history.
type LocalWorkflowDecision struct {
	ID              string    `json:"id"`
	WorkflowID      string    `json:"workflowId"`
	RequestID       string    `json:"requestId"`
	Kind            string    `json:"kind" enum:"accepted,changes_requested"`
	Comment         string    `json:"comment"`
	Source          string    `json:"source" enum:"desktop_user"`
	Cycle           int       `json:"cycle"`
	ExpectedVersion int64     `json:"expectedVersion"`
	SnapshotID      string    `json:"snapshotId"`
	TestOperationID string    `json:"testOperationId"`
	ReviewRunID     string    `json:"reviewRunId"`
	CreatedAt       time.Time `json:"createdAt"`
}

// SameRequest compares immutable inputs without a newly generated ID or clock.
func (d LocalWorkflowDecision) SameRequest(other LocalWorkflowDecision) bool {
	return d.WorkflowID == other.WorkflowID && d.RequestID == other.RequestID && d.Kind == other.Kind && d.Comment == other.Comment && d.Source == other.Source && d.Cycle == other.Cycle && d.ExpectedVersion == other.ExpectedVersion && d.SnapshotID == other.SnapshotID && d.TestOperationID == other.TestOperationID && d.ReviewRunID == other.ReviewRunID
}

// Local workflow execution phases are durable checkpoints, not session display states.
const (
	WorkflowWaiting   LocalWorkflowPhase = "waiting_executor"
	WorkflowTesting   LocalWorkflowPhase = "testing"
	WorkflowReviewing LocalWorkflowPhase = "reviewing"
	WorkflowFixing    LocalWorkflowPhase = "fixing"
	WorkflowReady     LocalWorkflowPhase = "ready"
	WorkflowBlocked   LocalWorkflowPhase = "blocked"
	WorkflowStopping  LocalWorkflowPhase = "stopping"
	WorkflowStopped   LocalWorkflowPhase = "stopped"
)

// LocalWorkflowOperation correlates one reserved side effect with its cycle and controller generation.
type LocalWorkflowOperation struct {
	InputOperationID string     `json:"inputOperationId,omitempty"`
	ID               string     `json:"id"`
	WorkflowID       string     `json:"workflowId"`
	Kind             string     `json:"kind" enum:"tests,review,fix"`
	Cycle            int        `json:"cycle"`
	Attempt          int        `json:"attempt"`
	State            string     `json:"state" enum:"reserved,running,succeeded,failed,interrupted,uncertain"`
	Generation       string     `json:"-"`
	ClientMessageID  string     `json:"-"`
	WorkerTurnID     string     `json:"workerTurnId,omitempty"`
	ReviewRunID      string     `json:"reviewRunId,omitempty"`
	SnapshotID       string     `json:"snapshotId,omitempty"`
	ObservationID    string     `json:"-"`
	ErrorCode        string     `json:"errorCode,omitempty"`
	ErrorMessage     string     `json:"errorMessage,omitempty"`
	StartedAt        time.Time  `json:"startedAt"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
}

// LocalWorkflowTestResult records one command outcome and its private log locations.
type LocalWorkflowTestResult struct {
	ID              string     `json:"id"`
	OperationID     string     `json:"operationId"`
	CommandIndex    int        `json:"commandIndex"`
	Command         string     `json:"command"`
	Status          string     `json:"status" enum:"succeeded,failed,interrupted,timed_out"`
	ExitCode        *int       `json:"exitCode,omitempty"`
	StdoutLog       string     `json:"-"`
	StderrLog       string     `json:"-"`
	StdoutTruncated bool       `json:"stdoutTruncated"`
	StderrTruncated bool       `json:"stderrTruncated"`
	StartedAt       time.Time  `json:"startedAt"`
	CompletedAt     *time.Time `json:"completedAt,omitempty"`
}

// EffectiveLocalWorkflowMaxRounds resolves the legacy zero value to three cycles.
func (c ProjectConfig) EffectiveLocalWorkflowMaxRounds() int {
	if c.LocalWorkflowMaxRounds == 0 {
		return 3
	}
	return c.LocalWorkflowMaxRounds
}

// LocalWorkflowWorkerGeneration identifies both the interface and its controller epoch.
func LocalWorkflowWorkerGeneration(r SessionRecord) string {
	if NormalizeSessionMode(r.Mode) == SessionModeChat {
		return "chat:" + r.Metadata.ControllerGeneration
	}
	return "tui:" + r.Metadata.RuntimeLaunchID
}
