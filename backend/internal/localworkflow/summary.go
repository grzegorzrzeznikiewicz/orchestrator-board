// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).
package localworkflow

import (
	"context"
	"fmt"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localsnapshot"
)

// MaxSummarySessions bounds both the batch query and its display response.
const MaxSummarySessions = 200

// CardRun includes only checkpoint fields used by the board, without task text,
// configuration, commands, review bodies or history.
type CardRun struct {
	ProviderRetry *domain.LocalWorkflowProviderRetry `json:"providerRetry,omitempty"`
	ID            string                             `json:"id"`
	Version       int64                              `json:"version"`
	Phase         domain.LocalWorkflowPhase          `json:"phase" enum:"waiting_executor,testing,reviewing,fixing,ready,blocked,stopping,stopped"`
	Cycle         int                                `json:"cycle"`
	MaxRounds     int                                `json:"maxRounds"`
	Automatic     bool                               `json:"automatic,omitempty"`
	PlanState     string                             `json:"planState,omitempty"`
	Profile       string                             `json:"profile,omitempty"`
	BlockCode     string                             `json:"blockCode,omitempty"`
	BlockMessage  string                             `json:"blockMessage,omitempty"`
}

// CardSummary is display-only. Current and Accepted reflect a previously
// validated proof with uninterrupted workspace observation; commands and native
// acceptance always read and validate the authoritative full Snapshot again.
type CardSummary struct {
	SessionID      domain.SessionID `json:"sessionId"`
	Run            *CardRun         `json:"run"`
	Stage          string           `json:"stage" enum:"executing,testing,local_review,fixing,awaiting_acceptance,accepted"`
	Current        bool             `json:"current"`
	Accepted       bool             `json:"accepted"`
	Automation     AutomationState  `json:"automation"`
	TestsPresent   bool             `json:"testsPresent"`
	TestsPassed    int              `json:"testsPassed"`
	TestsTotal     int              `json:"testsTotal"`
	TestsQueued    bool             `json:"testsQueued"`
	RequiredCount  int              `json:"requiredCount"`
	RequiredReason string           `json:"requiredReason,omitempty"`
}

type cardFactsReader interface {
	ListLocalWorkflowCardFacts(context.Context, []domain.SessionID) ([]domain.LocalWorkflowCardFacts, error)
}

type cardProof struct {
	runID    string
	version  int64
	reviewID string
	guard    *localsnapshot.Guard
	current  bool
	accepted bool
}

// SummarySessionIDs validates before storage access and preserves stable order.
func SummarySessionIDs(ids []domain.SessionID) ([]domain.SessionID, error) {
	if len(ids) == 0 || len(ids) > MaxSummarySessions {
		return nil, fmt.Errorf("%w: provide 1 to %d session IDs", domain.ErrLocalWorkflowInvalid, MaxSummarySessions)
	}
	out := make([]domain.SessionID, 0, len(ids))
	seen := make(map[domain.SessionID]bool, len(ids))
	for _, id := range ids {
		if string(id) != strings.TrimSpace(string(id)) || strings.ContainsAny(string(id), "\x00\r\n,") || !safeOperationID(string(id)) {
			return nil, fmt.Errorf("%w: invalid session ID", domain.ErrLocalWorkflowInvalid)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// Summaries reads one bounded batch and never loads full evidence or hashes a
// workspace just to display a card. Missing proofs remain conservative.
func (c *Coordinator) Summaries(ctx context.Context, ids []domain.SessionID) ([]CardSummary, error) {
	ids, err := SummarySessionIDs(ids)
	if err != nil {
		return nil, err
	}
	reader, ok := c.deps.Store.(cardFactsReader)
	if !ok {
		return nil, fmt.Errorf("local workflow card projection is unavailable")
	}
	facts, err := reader.ListLocalWorkflowCardFacts(ctx, ids)
	if err != nil {
		return nil, err
	}
	cards := make([]CardSummary, 0, len(facts))
	for _, fact := range facts {
		card := CardSummary{SessionID: fact.SessionID, Stage: "executing", TestsPresent: fact.TestOperationID != "", TestsPassed: fact.TestsPassed, TestsTotal: fact.TestsTotal, RequiredCount: fact.RequiredCount, RequiredReason: cardFindingPreview(fact.RequiredReason), Automation: AutomationState{Pending: fact.PendingIntent}}
		if r := fact.Run; r != nil {
			card.Run = &CardRun{ID: r.ID, Version: r.Version, Phase: r.Phase, Cycle: r.Cycle, MaxRounds: r.MaxRounds, Automatic: r.Automatic, PlanState: r.PlanState, ProviderRetry: r.ProviderRetry, BlockCode: r.BlockCode, BlockMessage: r.BlockMessage}
			if r.VerificationPlan != nil {
				card.Run.Profile = r.VerificationPlan.Profile
			}
			if waiting, ok := c.deps.Tests.(interface{ Waiting(string) bool }); ok && fact.TestOperationID != "" && r.Phase == domain.WorkflowTesting {
				card.TestsQueued = waiting.Waiting(fact.TestOperationID)
			}
			c.mu.Lock()
			proof, known := c.summaryProofs[fact.SessionID]
			guard := c.guards[r.ID]
			c.mu.Unlock()
			if known && proof.runID == r.ID && proof.version == r.Version &&
				proof.reviewID != "" && proof.reviewID == fact.ReviewRunID &&
				fact.ExecutorIdle && fact.ReviewValid && !fact.PendingIntent {
				// The watcher identity prevents reviving proof after lost continuity.
				if proof.current && proof.guard != nil && proof.guard == guard && guard.CheckObserved() == nil {
					card.Current = true
					card.Accepted = proof.accepted
				}
			}
			card.Stage = workflowStage(Snapshot{Run: r, Current: card.Current, Accepted: card.Accepted, Operations: []domain.LocalWorkflowOperation{{Kind: fact.LatestOperationKind}}})
			if fact.PendingIntent && r.StopRequested {
				card.Automation.BlockMessage = r.BlockMessage
			}
		}
		if fact.PendingIntent {
			card.Stage = "executing"
			card.Current = false
			card.Accepted = false
		}
		cards = append(cards, card)
	}
	return cards, nil
}

// rememberSummary receives authoritative validation results. It does not make
// their readiness reusable by Get or Decide and keeps at most one per session.
func (c *Coordinator) rememberSummary(s Snapshot) {
	if s.Run == nil {
		return
	}
	r := s.Run
	proof := cardProof{runID: r.ID, version: r.Version, current: s.Current, accepted: s.Accepted}
	if review := latestOperation(s.Operations, "review", r.Cycle); review != nil {
		proof.reviewID = review.ReviewRunID
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	proof.guard = c.guards[r.ID]
	if c.summaryProofs == nil {
		c.summaryProofs = map[domain.SessionID]cardProof{}
	}
	c.summaryProofs[r.SessionID] = proof
}

// rememberReadySummary seeds a newly completed checkpoint once, so Awaiting
// acceptance is visible even before its details are opened. It uses the review
// already validated by the transition, without loading older reviews/history.
func (c *Coordinator) rememberReadySummary(ctx context.Context, r domain.LocalWorkflowRun, result domain.ReviewRun) {
	ops, err := c.deps.Store.ListLocalWorkflowOperations(ctx, r.ID)
	if err != nil {
		return
	}
	tests, err := c.deps.Store.ListLocalWorkflowTests(ctx, r.ID)
	if err != nil {
		return
	}
	s := Snapshot{Run: &r, Operations: ops, Tests: tests, Reviews: []domain.ReviewRun{result}}
	s.Current = currentEvidence(s) && c.checkGuard(ctx, r.ID) == nil
	c.rememberSummary(s)
}

// cardFindingPreview also bounds non-SQL readers without retaining complete
// findings in the proof cache. Slice at a rune boundary to preserve UTF-8 text.
func cardFindingPreview(value string) string {
	count := 0
	for offset := range value {
		if count == 1024 {
			return strings.Clone(value[:offset])
		}
		count++
	}
	return value
}
