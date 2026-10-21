// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).
package localworkflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type cardFactsStore struct {
	Store
	facts []domain.LocalWorkflowCardFacts
	calls int
	ids   []domain.SessionID
}

func (s *cardFactsStore) ListLocalWorkflowCardFacts(_ context.Context, ids []domain.SessionID) ([]domain.LocalWorkflowCardFacts, error) {
	s.calls++
	s.ids = append([]domain.SessionID{}, ids...)
	return s.facts, nil
}
func (s *cardFactsStore) ListLocalWorkflowOperations(context.Context, string) ([]domain.LocalWorkflowOperation, error) {
	panic("card must not read operation history")
}
func (s *cardFactsStore) ListLocalWorkflowTests(context.Context, string) ([]domain.LocalWorkflowTestResult, error) {
	panic("card must not read test history")
}
func (s *cardFactsStore) ListLocalWorkflowDecisions(context.Context, string) ([]domain.LocalWorkflowDecision, error) {
	panic("card must not read decision history")
}

type noCardReviews struct{}

func (noCardReviews) TriggerWorkflowLocal(context.Context, domain.LocalWorkflowRun, string, string) (domain.ReviewRun, error) {
	panic("card must not trigger review")
}
func (noCardReviews) GetWorkflowLocal(context.Context, string) (domain.ReviewRun, error) {
	panic("card must not load review body")
}
func (noCardReviews) CancelWorkflowLocal(context.Context, string) error {
	panic("card must not cancel review")
}

func TestSummariesReadOneBoundedBatchWithoutFullEvidence(t *testing.T) {
	store := &cardFactsStore{facts: []domain.LocalWorkflowCardFacts{
		{SessionID: "one", Run: &domain.LocalWorkflowRun{ID: "wf", SessionID: "one", Version: 7, Phase: domain.WorkflowTesting, Cycle: 2, MaxRounds: 3}, TestOperationID: "tests", TestsPassed: 1, TestsTotal: 2},
		{SessionID: "two"},
	}}
	c := New(Deps{Store: store, Reviews: noCardReviews{}})
	cards, err := c.Summaries(context.Background(), []domain.SessionID{"one", "two", "one"})
	if err != nil {
		t.Fatal(err)
	}
	if store.calls != 1 || len(store.ids) != 2 || len(cards) != 2 {
		t.Fatalf("not a single deduplicated batch: calls=%d ids=%v cards=%+v", store.calls, store.ids, cards)
	}
	if cards[0].Stage != "testing" || cards[0].TestsPassed != 1 || cards[0].TestsTotal != 2 || !cards[0].TestsPresent || cards[1].Stage != "executing" {
		t.Fatalf("incorrect projection: %+v", cards)
	}
}

func TestSummariesExposeDurableProviderRetryWithoutReadingEvidence(t *testing.T) {
	next := time.Date(2026, 10, 20, 10, 2, 0, 0, time.UTC)
	retry := &domain.LocalWorkflowProviderRetry{Stage: "planner", State: "waiting", Reason: domain.ProviderAccessRateLimited, Attempt: 1, MaxAttempts: 3, FailureAt: next.Add(-time.Minute), NextAt: &next, AttemptID: "reserved", ContextID: strings.Repeat("a", 64)}
	store := &cardFactsStore{facts: []domain.LocalWorkflowCardFacts{{SessionID: "one", Run: &domain.LocalWorkflowRun{ID: "wf", SessionID: "one", Version: 4, Phase: domain.WorkflowBlocked, BlockCode: "provider_waiting", ProviderRetry: retry}}}}
	c := New(Deps{Store: store, Reviews: noCardReviews{}})
	cards, err := c.Summaries(t.Context(), []domain.SessionID{"one"})
	if err != nil || len(cards) != 1 || cards[0].Run == nil {
		t.Fatalf("summary: %+v %v", cards, err)
	}
	if cards[0].Run.ProviderRetry == nil || *cards[0].Run.ProviderRetry != *retry {
		t.Fatalf("lost retry facts: %+v", cards[0].Run)
	}
	if cards[0].Current || cards[0].Accepted || store.calls != 1 {
		t.Fatalf("retry changed proof or batching: %+v calls=%d", cards, store.calls)
	}
}
func TestSummariesRejectUnboundedOrInvalidIDsBeforeStorage(t *testing.T) {
	store := &cardFactsStore{}
	c := New(Deps{Store: store})
	ids := make([]domain.SessionID, 201)
	for i := range ids {
		ids[i] = "worker"
	}
	for _, input := range [][]domain.SessionID{ids, {""}, {"bad\nidentifier"}, {" worker"}} {
		if _, err := c.Summaries(context.Background(), input); !errors.Is(err, domain.ErrLocalWorkflowInvalid) {
			t.Fatalf("invalid IDs accepted: %v err=%v", input, err)
		}
	}
	if store.calls != 0 {
		t.Fatal("invalid request reached storage")
	}
}
func TestSummariesKeepValidatedReadyOnlyWhileProofIsContinuous(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	ctx := context.Background()
	if _, err := f.coordinator.Start(ctx, f.session.ID, "summary-proof"); err != nil {
		t.Fatal(err)
	}
	ready := awaitPhase(t, f, domain.WorkflowReady)
	store := &cardFactsStore{Store: f.store, facts: []domain.LocalWorkflowCardFacts{{SessionID: f.session.ID, Run: ready.Run, TestOperationID: latestOperation(ready.Operations, "tests", ready.Run.Cycle).ID, TestsPassed: 1, TestsTotal: 1, ExecutorIdle: true, ReviewValid: true, ReviewRunID: latestOperation(ready.Operations, "review", ready.Run.Cycle).ReviewRunID}}}
	f.coordinator.deps.Store = store
	cards, err := f.coordinator.Summaries(ctx, []domain.SessionID{f.session.ID})
	if err != nil || len(cards) != 1 || cards[0].Stage != "awaiting_acceptance" || !cards[0].Current {
		t.Fatalf("valid proof disappeared: %+v %v", cards, err)
	}
	store.facts[0].PendingIntent = true
	cards, err = f.coordinator.Summaries(ctx, []domain.SessionID{f.session.ID})
	if err != nil || cards[0].Current || cards[0].Accepted || cards[0].Stage != "executing" || !cards[0].Automation.Pending {
		t.Fatalf("new human intent retained old readiness: %+v %v", cards, err)
	}
	store.facts[0].PendingIntent = false
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("edited"), 0600); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(time.Second)
	for !f.coordinator.guard(ready.Run.ID).Changed() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	cards, err = f.coordinator.Summaries(ctx, []domain.SessionID{f.session.ID})
	if err != nil || cards[0].Current || cards[0].Stage != "local_review" {
		t.Fatalf("workspace mutation retained readiness: %+v %v", cards, err)
	}
	// A fresh coordinator has no continuous observation or validated proof.
	restarted := New(Deps{Store: store, Reviews: noCardReviews{}})
	cards, err = restarted.Summaries(ctx, []domain.SessionID{f.session.ID})
	if err != nil || cards[0].Current || cards[0].Stage != "local_review" {
		t.Fatalf("restart revived readiness: %+v %v", cards, err)
	}
}

func TestSummariesPublishReadyWithoutOpeningFullDetails(t *testing.T) {
	f := setupWorkflow(t, []string{"test -f value"}, 3)
	ctx := context.Background()
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	start, err := f.coordinator.Start(ctx, f.session.ID, "ready-card")
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	var run domain.LocalWorkflowRun
	for time.Now().Before(deadline) {
		var found bool
		run, found, err = f.store.CurrentLocalWorkflow(ctx, f.session.ID)
		if err != nil || !found {
			t.Fatalf("run missing: %v", err)
		}
		if run.Phase == domain.WorkflowReady {
			break
		}
		if err = f.coordinator.Evaluate(ctx, start.Run.ID); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond)
	}
	if run.Phase != domain.WorkflowReady {
		t.Fatalf("not ready: %+v", run)
	}
	ops, err := f.store.ListLocalWorkflowOperations(ctx, run.ID)
	if err != nil {
		t.Fatal(err)
	}
	f.coordinator.deps.Store = &cardFactsStore{Store: f.store, facts: []domain.LocalWorkflowCardFacts{{SessionID: f.session.ID, Run: &run, ExecutorIdle: true, ReviewValid: true, ReviewRunID: latestOperation(ops, "review", run.Cycle).ReviewRunID}}}
	cards, err := f.coordinator.Summaries(ctx, []domain.SessionID{f.session.ID})
	if err != nil || len(cards) != 1 || cards[0].Stage != "awaiting_acceptance" {
		t.Fatalf("ready card needs full detail query: %+v %v", cards, err)
	}
}

func TestSummariesAcceptedProofCannotSurviveVersionOrGuardReplacement(t *testing.T) {
	f, _, in := readyHumanDecision(t, 3)
	f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { return nil })
	accepted, err := f.coordinator.Decide(context.Background(), f.session.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	run := *accepted.Run
	store := &cardFactsStore{Store: f.store, facts: []domain.LocalWorkflowCardFacts{{SessionID: f.session.ID, Run: &run, ExecutorIdle: true, ReviewValid: true, ReviewRunID: in.ReviewRunID}}}
	f.coordinator.deps.Store = store
	cards, err := f.coordinator.Summaries(context.Background(), []domain.SessionID{f.session.ID})
	if err != nil || !cards[0].Accepted || cards[0].Stage != "accepted" {
		t.Fatalf("acceptance lost: %+v %v", cards, err)
	}
	run.Version++
	cards, err = f.coordinator.Summaries(context.Background(), []domain.SessionID{f.session.ID})
	if err != nil || cards[0].Current || cards[0].Accepted {
		t.Fatalf("old proof used for new version: %+v %v", cards, err)
	}
	run.Version--
	f.coordinator.closeGuard(run.ID)
	cards, err = f.coordinator.Summaries(context.Background(), []domain.SessionID{f.session.ID})
	if err != nil || cards[0].Current || cards[0].Accepted {
		t.Fatalf("closed observation retained acceptance: %+v %v", cards, err)
	}
}

func TestSummariesRejectReadyProofWhenExecutorOrReviewChanges(t *testing.T) {
	f, _, in := readyHumanDecision(t, 3)
	f.coordinator.deps.Decisions = confirmDecisionFunc(func(context.Context, DecisionChallenge) error { return nil })
	accepted, err := f.coordinator.Decide(context.Background(), f.session.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	valid := domain.LocalWorkflowCardFacts{SessionID: f.session.ID, Run: accepted.Run, ExecutorIdle: true, ReviewValid: true, ReviewRunID: in.ReviewRunID}
	store := &cardFactsStore{Store: f.store, facts: []domain.LocalWorkflowCardFacts{valid}}
	f.coordinator.deps.Store = store
	for _, tc := range []struct {
		name   string
		mutate func(*domain.LocalWorkflowCardFacts)
	}{
		{"busy executor", func(f *domain.LocalWorkflowCardFacts) { f.ExecutorIdle = false }},
		{"terminated executor", func(f *domain.LocalWorkflowCardFacts) { f.ExecutorIdle = false }},
		{"invalidated review", func(f *domain.LocalWorkflowCardFacts) { f.ReviewValid = false }},
		{"replaced review", func(f *domain.LocalWorkflowCardFacts) { f.ReviewRunID = "replacement" }},
		{"missing review", func(f *domain.LocalWorkflowCardFacts) { f.ReviewRunID = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store.facts[0] = valid
			tc.mutate(&store.facts[0])
			cards, err := f.coordinator.Summaries(context.Background(), []domain.SessionID{f.session.ID})
			if err != nil || len(cards) != 1 || cards[0].Current || cards[0].Accepted || cards[0].Stage != "local_review" {
				t.Fatalf("obsolete accepted proof remains current: %+v %v", cards, err)
			}
		})
	}
}

func TestSummariesShowRequiredFindingsWithoutOpeningDetailsAndBoundPreview(t *testing.T) {
	reason := strings.Repeat("ą界🙂", 500) + "must remain complete in full details"
	store := &cardFactsStore{facts: []domain.LocalWorkflowCardFacts{{SessionID: "worker", Run: &domain.LocalWorkflowRun{ID: "wf", SessionID: "worker", Version: 9, Phase: domain.WorkflowFixing, Cycle: 1, MaxRounds: 3}, RequiredCount: 2, RequiredReason: reason}}}
	c := New(Deps{Store: store, Reviews: noCardReviews{}})
	cards, err := c.Summaries(context.Background(), []domain.SessionID{"worker"})
	if err != nil || len(cards) != 1 || cards[0].RequiredCount != 2 || cards[0].RequiredReason == "" {
		t.Fatalf("required finding needs full detail read: %+v %v", cards, err)
	}
	if !utf8.ValidString(cards[0].RequiredReason) || utf8.RuneCountInString(cards[0].RequiredReason) != 1024 || !strings.HasPrefix(reason, cards[0].RequiredReason) {
		t.Fatalf("preview is unbounded or corrupt: bytes=%d runes=%d", len(cards[0].RequiredReason), utf8.RuneCountInString(cards[0].RequiredReason))
	}
	if store.facts[0].RequiredReason != reason {
		t.Fatal("card truncation modified full finding text")
	}
}
