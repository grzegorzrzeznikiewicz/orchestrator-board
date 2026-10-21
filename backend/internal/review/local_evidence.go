// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package review

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const (
	maxEvidenceWorkflows  = 6
	maxEvidenceOperations = 24
	maxEvidenceTests      = 24
	maxEvidenceReviews    = 12
	maxEvidenceDecisions  = 4
	maxEvidenceBytes      = 64 * 1024
	maxEvidenceLogBytes   = 64 * 1024
	maxEvidenceSummaries  = 8
)

// LocalReviewEvidence is an immutable, read-only launch-time projection. Never
// embed persistence records here: those contain environment, commands, paths,
// transcripts and review bodies. Recorded checkpoints are not current readiness.
type LocalReviewEvidence struct {
	Source           string             `json:"source"`
	CapturedAt       time.Time          `json:"capturedAt"`
	SessionID        domain.SessionID   `json:"sessionId"`
	WorkflowID       string             `json:"workflowId"`
	ReviewRunID      string             `json:"reviewRunId"`
	TargetSnapshotID string             `json:"targetSnapshotId"`
	Workflows        []workflowEvidence `json:"workflows"`
	OmittedWorkflows int                `json:"omittedWorkflows"`
	Limits           evidenceLimits     `json:"limits"`
}

type evidenceLimits struct {
	Workflows             int `json:"workflows"`
	OperationsPerWorkflow int `json:"operationsPerWorkflow"`
	TestsPerWorkflow      int `json:"testsPerWorkflow"`
	ReviewsPerWorkflow    int `json:"reviewsPerWorkflow"`
	DecisionsPerWorkflow  int `json:"decisionsPerWorkflow"`
	Bytes                 int `json:"bytes"`
	LogBytesPerStream     int `json:"logBytesPerStream"`
	SummariesPerTest      int `json:"summariesPerTest"`
}

type workflowEvidence struct {
	WorkflowID        string                    `json:"workflowId"`
	Historical        bool                      `json:"historical"`
	Version           int64                     `json:"version"`
	Phase             domain.LocalWorkflowPhase `json:"phase"`
	Cycle             int                       `json:"cycle"`
	Profile           string                    `json:"profile"`
	CreatedAt         time.Time                 `json:"createdAt"`
	UpdatedAt         time.Time                 `json:"updatedAt"`
	ClosedAt          *time.Time                `json:"closedAt,omitempty"`
	Operations        []operationEvidence       `json:"operations"`
	Tests             []testEvidence            `json:"tests"`
	Reviews           []reviewEvidence          `json:"reviews"`
	Decisions         []decisionEvidence        `json:"decisions"`
	OmittedOperations int                       `json:"omittedOperations"`
	OmittedTests      int                       `json:"omittedTests"`
	OmittedReviews    int                       `json:"omittedReviews"`
	OmittedDecisions  int                       `json:"omittedDecisions"`
}

type operationEvidence struct {
	OperationID string     `json:"operationId"`
	Kind        string     `json:"kind"`
	State       string     `json:"state"`
	Cycle       int        `json:"cycle"`
	SnapshotID  string     `json:"snapshotId,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

type testEvidence struct {
	TestID               string              `json:"testId"`
	OperationID          string              `json:"operationId"`
	CommandIndex         int                 `json:"commandIndex"`
	Status               string              `json:"status"`
	ExitCode             *int                `json:"exitCode,omitempty"`
	StartedAt            time.Time           `json:"startedAt"`
	CompletedAt          *time.Time          `json:"completedAt,omitempty"`
	Counts               []testCountEvidence `json:"counts"`
	OutputOmitted        bool                `json:"outputOmitted"`
	SummariesUnavailable bool                `json:"summariesUnavailable"`
}

// Counts are parsed from observed command output, never executor assertions.
// Only numbers and fixed labels survive; raw log lines cannot enter the prompt.
type testCountEvidence struct {
	Stream  string `json:"stream"`
	Kind    string `json:"kind"`
	Passed  int    `json:"passed"`
	Failed  int    `json:"failed"`
	Skipped int    `json:"skipped"`
}

type reviewEvidence struct {
	ReviewRunID         string                 `json:"reviewRunId"`
	WorkflowOperationID string                 `json:"workflowOperationId"`
	SnapshotID          string                 `json:"snapshotId"`
	Status              domain.ReviewRunStatus `json:"status"`
	Verdict             domain.ReviewVerdict   `json:"verdict"`
	Harness             domain.ReviewerHarness `json:"harness"`
	CreatedAt           time.Time              `json:"createdAt"`
	InvalidatedAt       *time.Time             `json:"invalidatedAt,omitempty"`
}

type decisionEvidence struct {
	DecisionID      string    `json:"decisionId"`
	Kind            string    `json:"kind"`
	Cycle           int       `json:"cycle"`
	SnapshotID      string    `json:"snapshotId"`
	TestOperationID string    `json:"testOperationId"`
	ReviewRunID     string    `json:"reviewRunId"`
	CreatedAt       time.Time `json:"createdAt"`
}

type localEvidenceStore interface {
	CurrentLocalWorkflow(context.Context, domain.SessionID) (domain.LocalWorkflowRun, bool, error)
	ListSessionLocalWorkflows(context.Context, domain.SessionID) ([]domain.LocalWorkflowRun, error)
	ListLocalWorkflowOperations(context.Context, string) ([]domain.LocalWorkflowOperation, error)
	ListLocalWorkflowTests(context.Context, string) ([]domain.LocalWorkflowTestResult, error)
	ListLocalWorkflowDecisions(context.Context, string) ([]domain.LocalWorkflowDecision, error)
}

func (e *Engine) reportEvidence(ctx context.Context, workflow domain.LocalWorkflowRun, review domain.ReviewRun) (*LocalReviewEvidence, error) {
	store, ok := e.store.(localEvidenceStore)
	if !ok {
		return nil, fmt.Errorf("%w: report evidence unavailable: workflow history store is unavailable", ErrInvalid)
	}
	current, found, err := store.CurrentLocalWorkflow(ctx, review.SessionID)
	if err != nil {
		return nil, err
	}
	if !found || current.ID != workflow.ID || current.SessionID != review.SessionID {
		return nil, fmt.Errorf("%w: report evidence unavailable: current workflow identity changed", ErrConflict)
	}
	history, err := store.ListSessionLocalWorkflows(ctx, review.SessionID)
	if err != nil {
		return nil, err
	}
	reviews, err := e.store.ListReviewRunsBySession(ctx, review.SessionID)
	if err != nil {
		return nil, err
	}
	out := &LocalReviewEvidence{Source: "daemon", CapturedAt: e.clock().UTC(), SessionID: review.SessionID, WorkflowID: current.ID, ReviewRunID: review.ID, TargetSnapshotID: review.SnapshotID, Limits: evidenceLimits{maxEvidenceWorkflows, maxEvidenceOperations, maxEvidenceTests, maxEvidenceReviews, maxEvidenceDecisions, maxEvidenceBytes, maxEvidenceLogBytes, maxEvidenceSummaries}}
	selected := []domain.LocalWorkflowRun{current}
	for _, run := range history {
		if run.ID == current.ID || run.SessionID != review.SessionID {
			continue
		}
		if len(selected) >= maxEvidenceWorkflows {
			out.OmittedWorkflows++
			continue
		}
		selected = append(selected, run)
	}
	for _, run := range selected {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		facts, err := e.workflowEvidence(ctx, store, run, reviews, review.ID, run.ID != current.ID)
		if err != nil {
			return nil, err
		}
		out.Workflows = append(out.Workflows, facts)
	}
	for len(out.Workflows) > 1 {
		encoded, err := json.Marshal(out)
		if err != nil {
			return nil, err
		}
		if len(encoded) <= maxEvidenceBytes {
			break
		}
		out.Workflows = out.Workflows[:len(out.Workflows)-1]
		out.OmittedWorkflows++
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(encoded) > maxEvidenceBytes {
		return nil, fmt.Errorf("%w: report evidence unavailable: current workflow evidence exceeds the size limit", ErrInvalid)
	}
	return out, nil
}

func (e *Engine) workflowEvidence(ctx context.Context, store localEvidenceStore, run domain.LocalWorkflowRun, reviews []domain.ReviewRun, activeReview string, historical bool) (workflowEvidence, error) {
	out := workflowEvidence{WorkflowID: run.ID, Historical: historical, Version: run.Version, Phase: run.Phase, Cycle: run.Cycle, Profile: "unknown", CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt, ClosedAt: run.ClosedAt}
	if run.VerificationPlan != nil {
		out.Profile = run.VerificationPlan.Profile
	}
	ops, err := store.ListLocalWorkflowOperations(ctx, run.ID)
	if err != nil {
		return out, err
	}
	ops = slices.Clone(ops)
	slices.Reverse(ops) // Persistence returns insertion order; prefer recent cycles.
	selected := map[string]domain.LocalWorkflowOperation{}
	for _, op := range ops {
		if op.WorkflowID != run.ID {
			continue
		}
		if len(out.Operations) >= maxEvidenceOperations {
			out.OmittedOperations++
			continue
		}
		selected[op.ID] = op
		out.Operations = append(out.Operations, operationEvidence{op.ID, op.Kind, op.State, op.Cycle, op.SnapshotID, op.StartedAt, op.CompletedAt})
	}
	tests, err := store.ListLocalWorkflowTests(ctx, run.ID)
	if err != nil {
		return out, err
	}
	tests = slices.Clone(tests)
	slices.Reverse(tests)
	for _, test := range tests {
		op, found := selected[test.OperationID]
		if !found || op.Kind != "tests" {
			out.OmittedTests++
			continue
		}
		if len(out.Tests) >= maxEvidenceTests {
			out.OmittedTests++
			continue
		}
		counts, omitted, unavailable := e.testCounts(test)
		out.Tests = append(out.Tests, testEvidence{test.ID, test.OperationID, test.CommandIndex, test.Status, test.ExitCode, test.StartedAt, test.CompletedAt, counts, omitted, unavailable})
	}
	for _, review := range reviews {
		if review.ID == activeReview || review.SessionID != run.SessionID || review.TargetKind != domain.ReviewTargetLocal {
			continue
		}
		if _, found := selected[review.WorkflowOperationID]; !found {
			continue
		}
		if len(out.Reviews) >= maxEvidenceReviews {
			out.OmittedReviews++
			continue
		}
		out.Reviews = append(out.Reviews, reviewEvidence{review.ID, review.WorkflowOperationID, review.SnapshotID, review.Status, review.Verdict, review.Harness, review.CreatedAt, review.InvalidatedAt})
	}
	decisions, err := store.ListLocalWorkflowDecisions(ctx, run.ID)
	if err != nil {
		return out, err
	}
	decisions = slices.Clone(decisions)
	slices.Reverse(decisions)
	for _, decision := range decisions {
		if decision.WorkflowID != run.ID || decision.Source != "desktop_user" {
			continue
		}
		if len(out.Decisions) >= maxEvidenceDecisions {
			out.OmittedDecisions++
			continue
		}
		out.Decisions = append(out.Decisions, decisionEvidence{decision.ID, decision.Kind, decision.Cycle, decision.SnapshotID, decision.TestOperationID, decision.ReviewRunID, decision.CreatedAt})
	}
	return out, nil
}

var countLine = regexp.MustCompile(`^(Test Files|Test Suites:|Tests:|Tests)\s+([0-9][0-9a-z ,|().]*)$`)
var pytestCountLine = regexp.MustCompile(`^(?:=+\s*)?([0-9]+ (?:passed|failed|skipped|warnings?)(?:, [0-9]+ (?:passed|failed|skipped|warnings?))*) in [0-9]+(?:\.[0-9]+)?s(?:\s*=+)?$`)
var countValue = regexp.MustCompile(`(\d+)\s+(passed|failed|skipped|pending|todo)\b`)
var countANSI = regexp.MustCompile(`\x1b\[[\x30-\x3f]*[\x20-\x2f]*[\x40-\x7e]`)

func (e *Engine) testCounts(test domain.LocalWorkflowTestResult) (counts []testCountEvidence, omitted, unavailable bool) {
	for _, stream := range []struct {
		name, path string
		truncated  bool
	}{{"stdout", test.StdoutLog, test.StdoutTruncated}, {"stderr", test.StderrLog, test.StderrTruncated}} {
		if stream.path == "" {
			continue
		}
		text, truncated, err := readEvidenceLog(e.localDataDir, stream.path)
		if err != nil {
			unavailable = true
			continue
		}
		omitted = omitted || truncated || stream.truncated
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(countANSI.ReplaceAllString(line, ""))
			match := countLine.FindStringSubmatch(line)
			kind := "tests"
			var summary string
			if match != nil {
				summary = match[2]
				switch match[1] {
				case "Test Files":
					kind = "test_files"
				case "Test Suites:":
					kind = "test_suites"
				}
			} else if pytest := pytestCountLine.FindStringSubmatch(line); pytest != nil {
				summary = pytest[1]
			} else {
				continue
			}
			values := countValue.FindAllStringSubmatch(summary, -1)
			if len(values) == 0 {
				continue
			}
			if len(counts) >= maxEvidenceSummaries {
				omitted = true
				continue
			}
			count := testCountEvidence{Stream: stream.name, Kind: kind}
			for _, value := range values {
				n, err := strconv.Atoi(value[1])
				if err != nil {
					continue
				}
				switch value[2] {
				case "passed":
					count.Passed += n
				case "failed":
					count.Failed += n
				default:
					count.Skipped += n
				}
			}
			counts = append(counts, count)
		}
	}
	return counts, omitted, unavailable || len(counts) == 0
}

func readEvidenceLog(dataDir, path string) (string, bool, error) {
	rel, err := filepath.Rel(dataDir, path)
	if err != nil {
		return "", false, err
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(rel)
	if err != nil {
		return "", false, err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, fmt.Errorf("test log is not a regular file")
	}
	truncated := info.Size() > maxEvidenceLogBytes
	if truncated {
		if _, err := file.Seek(-maxEvidenceLogBytes, io.SeekEnd); err != nil {
			return "", false, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(file, maxEvidenceLogBytes))
	if err != nil {
		return "", false, err
	}
	if truncated {
		if i := slices.Index(data, byte('\n')); i >= 0 {
			data = data[i+1:]
		}
	}
	return string(data), truncated, nil
}
