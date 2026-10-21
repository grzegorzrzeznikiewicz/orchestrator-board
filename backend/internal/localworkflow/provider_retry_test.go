// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package localworkflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type retryClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *retryClock) now() time.Time      { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *retryClock) add(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.at = c.at.Add(d) }

func transientProvider() error {
	return &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication, SafeToRetry: true, Cause: errors.New("private native diagnostic")}
}

func waitRetry(t *testing.T, f *workflowFixture, predicate func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		s, err := f.coordinator.Get(t.Context(), f.session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if predicate(s) {
			return s
		}
		if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
			t.Fatal(err)
		}
		time.Sleep(5 * time.Millisecond)
	}
	s, _ := f.coordinator.Get(t.Context(), f.session.ID)
	t.Fatalf("retry checkpoint not reached: %+v", s.Run)
	return Snapshot{}
}

func retryFixture(t *testing.T, planner plannerFunc) (*workflowFixture, *retryClock, Snapshot) {
	t.Helper()
	f := setupWorkflow(t, nil, 3)
	clock := &retryClock{at: time.Now().UTC()}
	f.coordinator.deps.Clock = clock.now
	f.coordinator.deps.Planner = planner
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "provider-recovery", "Produce a report")
	if err != nil {
		t.Fatal(err)
	}
	if err = f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
		t.Fatal(err)
	}
	return f, clock, s
}

func reportVerification() domain.LocalVerificationPlan {
	return domain.LocalVerificationPlan{Profile: "report", Rationale: "Written report", Commands: []string{}, Checks: []string{"Completeness"}}
}

func TestProviderRetryPlannerBackoffAndHumanAcceptance(t *testing.T) {
	var calls atomic.Int32
	f, clock, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		if calls.Add(1) == 1 {
			return domain.LocalVerificationPlan{}, transientProvider()
		}
		return reportVerification(), nil
	})
	s := waitRetry(t, f, func(s Snapshot) bool { return s.Run.Phase == domain.WorkflowBlocked })
	if s.Run.ProviderRetry == nil {
		t.Fatal("transient provider failure has no recovery checkpoint")
	}
	r := s.Run.ProviderRetry
	if r.State != "waiting" || r.Stage != "planner" || r.Attempt != 0 || r.MaxAttempts != 3 || r.NextAt == nil || !r.NextAt.Equal(clock.now().Add(30*time.Second)) {
		t.Fatalf("retry=%+v", r)
	}
	if s.Run.BlockMessage == "private native diagnostic" {
		t.Fatal("raw diagnostic exposed")
	}
	for i := 0; i < 20; i++ {
		if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
			t.Fatal(err)
		}
	}
	if calls.Load() != 1 {
		t.Fatal("polling bypassed backoff")
	}
	clock.add(30 * time.Second)
	if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
		t.Fatal(err)
	}
	s = waitRetry(t, f, func(s Snapshot) bool { return s.Run.VerificationPlan != nil })
	if calls.Load() != 2 || s.Run.ProviderRetry.Attempt != 1 || s.Run.ProviderRetry.State != "recovered" || s.Run.ProviderRetry.NextAt != nil {
		t.Fatalf("not recovered: %+v calls=%d", s.Run, calls.Load())
	}
	s = driveAutomaticReady(t, f, s.Run.ID)
	if !s.Current || s.Accepted || s.Run.Cycle != 1 || len(s.Decisions) != 0 {
		t.Fatal("provider recovery bypassed verification or human acceptance")
	}
}

func TestProviderRetryExhaustionSurvivesRestartAndConcurrentPolls(t *testing.T) {
	var calls atomic.Int32
	f, clock, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		calls.Add(1)
		return domain.LocalVerificationPlan{}, transientProvider()
	})
	s := waitRetry(t, f, func(s Snapshot) bool { return s.Run.Phase == domain.WorkflowBlocked })
	for attempt, delay := range []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second} {
		if err := f.coordinator.Recover(t.Context()); err != nil {
			t.Fatal(err)
		}
		clock.add(delay)
		var wg sync.WaitGroup
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() { defer wg.Done(); _ = f.coordinator.Evaluate(t.Context(), s.Run.ID) }()
		}
		wg.Wait()
		want := attempt + 1
		s = waitRetry(t, f, func(s Snapshot) bool {
			return s.Run.ProviderRetry != nil && s.Run.ProviderRetry.Attempt == want && s.Run.ProviderRetry.State != "checking"
		})
		if calls.Load() != int32(want+1) {
			t.Fatalf("duplicate attempt: %d", calls.Load())
		}
	}
	if s.Run.ProviderRetry.State != "exhausted" || s.Run.ProviderRetry.NextAt != nil || s.Run.BlockCode != "provider_exhausted" {
		t.Fatalf("not exhausted: %+v", s.Run)
	}
	clock.add(time.Hour)
	for i := 0; i < 5; i++ {
		_ = f.coordinator.Evaluate(t.Context(), s.Run.ID)
	}
	if calls.Load() != 4 {
		t.Fatal("exhausted budget reset")
	}
}

func TestProviderRetryRejectsOpaqueUnsafeAndUncertainFailures(t *testing.T) {
	for name, cause := range map[string]error{
		"opaque":       fmt.Errorf("HTTP 401 unauthorized quota 503"),
		"unsafe":       &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication},
		"uncertain":    errors.Join(transientProvider(), domain.ErrProviderExecutionUncertain),
		"invalid-plan": nil,
	} {
		t.Run(name, func(t *testing.T) {
			f, clock, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
				return domain.LocalVerificationPlan{}, cause
			})
			s := waitRetry(t, f, func(s Snapshot) bool { return s.Run.Phase == domain.WorkflowBlocked })
			if s.Run.ProviderRetry != nil {
				t.Fatalf("unsafe failure scheduled: %+v", s.Run.ProviderRetry)
			}
			clock.add(time.Hour)
			_ = f.coordinator.Evaluate(t.Context(), s.Run.ID)
			if s.Run.BlockCode != "verification_plan" && !errors.Is(cause, domain.ErrProviderExecutionUncertain) {
				t.Fatalf("wrong block: %+v", s.Run)
			}
		})
	}
}

func TestProviderRetryStopRetainsBackgroundTerminationUncertainty(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	f, _, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		close(entered)
		<-release
		return domain.LocalVerificationPlan{}, errors.Join(transientProvider(), domain.ErrProviderExecutionUncertain)
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("planner did not start")
	}
	s, _ := f.coordinator.Get(t.Context(), f.session.ID)
	stopping, err := f.coordinator.Stop(t.Context(), f.session.ID, s.Run.Version)
	if err != nil || stopping.Run.Phase != domain.WorkflowStopping {
		t.Fatalf("premature stop: %+v %v", stopping.Run, err)
	}
	close(release)
	s = waitRetry(t, f, func(s Snapshot) bool { return s.Run.BlockCode == "provider_execution_uncertain" })
	if !s.Run.StopRequested || s.Run.Phase != domain.WorkflowBlocked || s.Current || s.Accepted {
		t.Fatalf("uncertain provider claimed stopped: %+v", s.Run)
	}
	if _, err = f.coordinator.Resume(t.Context(), f.session.ID, s.Run.Version, nil); err == nil {
		t.Fatal("uncertain background process permitted a duplicate planner")
	}
}

func TestProviderRetryInterruptedAttemptIsNotReplayedAfterRestart(t *testing.T) {
	f, clock, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		return domain.LocalVerificationPlan{}, transientProvider()
	})
	s := waitRetry(t, f, func(s Snapshot) bool { return s.Run.Phase == domain.WorkflowBlocked })
	if s.Run.ProviderRetry == nil {
		t.Fatal("retry not persisted")
	}
	r := *s.Run
	r.ProviderRetry.State = "checking"
	r.ProviderRetry.Attempt = 1
	r.ProviderRetry.AttemptID = "interrupted-probe"
	r.ProviderRetry.NextAt = nil
	if ok, err := f.store.AdvanceLocalWorkflow(t.Context(), r.Version, r, nil); err != nil || !ok {
		t.Fatal(err)
	}
	replacement := New(f.coordinator.deps)
	t.Cleanup(func() { _ = replacement.Close(context.Background()) })
	f.coordinator = replacement
	if err := replacement.Recover(t.Context()); err != nil {
		t.Fatal(err)
	}
	clock.add(time.Hour)
	_ = f.coordinator.Evaluate(t.Context(), r.ID)
	s, _ = f.coordinator.Get(t.Context(), f.session.ID)
	if s.Run.ProviderRetry.State != "uncertain" || s.Run.BlockCode != "provider_uncertain" || s.Run.ProviderRetry.Attempt != 1 {
		t.Fatalf("uncertain request replayed: %+v", s.Run)
	}
}

type recoveringReviews struct {
	*scenarioReviews
	launches     atomic.Int32
	checks       atomic.Int32
	entered      chan struct{}
	release      chan struct{}
	checkFailure bool
}

func (r *recoveringReviews) TriggerWorkflowLocal(ctx context.Context, run domain.LocalWorkflowRun, id, snapshot string) (domain.ReviewRun, error) {
	if r.launches.Add(1) == 1 {
		return domain.ReviewRun{ID: "failed-preflight", Status: domain.ReviewRunFailed}, transientProvider()
	}
	return r.scenarioReviews.TriggerWorkflowLocal(ctx, run, id, snapshot)
}
func (r *recoveringReviews) CheckWorkflowProvider(context.Context, domain.LocalWorkflowRun) error {
	r.checks.Add(1)
	if r.entered != nil {
		close(r.entered)
		<-r.release
	}
	if r.checkFailure {
		return transientProvider()
	}
	return nil
}

func TestProviderRetryBudgetIsSharedAcrossPlannerAndReviewer(t *testing.T) {
	var plans atomic.Int32
	f, clock, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		if plans.Add(1) == 1 {
			return domain.LocalVerificationPlan{}, transientProvider()
		}
		return reportVerification(), nil
	})
	reviews := &recoveringReviews{scenarioReviews: f.reviews, checkFailure: true}
	f.coordinator.deps.Reviews = reviews
	waitRetry(t, f, func(s Snapshot) bool { return s.Run.BlockCode == "provider_waiting" })
	clock.add(30 * time.Second)
	s := waitRetry(t, f, func(s Snapshot) bool {
		return s.Run.ProviderRetry != nil && s.Run.ProviderRetry.Stage == "review" && s.Run.ProviderRetry.State == "waiting"
	})
	if s.Run.ProviderRetry.Attempt != 1 || !s.Run.ProviderRetry.NextAt.Equal(clock.now().Add(time.Minute)) {
		t.Fatal("reviewer reset planner's spent budget")
	}
	for attempt, delay := range []time.Duration{time.Minute, 2 * time.Minute} {
		clock.add(delay)
		want := attempt + 2
		s = waitRetry(t, f, func(s Snapshot) bool {
			return s.Run.ProviderRetry.Attempt == want && s.Run.ProviderRetry.State != "checking"
		})
	}
	if s.Run.BlockCode != "provider_exhausted" || s.Run.ProviderRetry.Attempt != 3 || reviews.checks.Load() != 2 || reviews.launches.Load() != 1 || plans.Load() != 2 {
		t.Fatalf("budget escaped stage boundary: %+v", s.Run.ProviderRetry)
	}
}

func TestProviderRetryReviewUsesFreshTestsAndReviewOperation(t *testing.T) {
	f, clock, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		return reportVerification(), nil
	})
	reviews := &recoveringReviews{scenarioReviews: f.reviews}
	f.coordinator.deps.Reviews = reviews
	s := waitRetry(t, f, func(s Snapshot) bool { return s.Run.BlockCode == "provider_waiting" })
	oldTests := latestOperation(s.Operations, "tests", 1)
	oldReview := latestOperation(s.Operations, "review", 1)
	if s.Run.PlanState != "complete" {
		t.Fatalf("reviewer access failure changed the completed plan state: %s", s.Run.PlanState)
	}
	if oldTests == nil || oldReview == nil || oldReview.ReviewRunID != "failed-preflight" || oldReview.State != "failed" {
		t.Fatal("failed preflight evidence missing")
	}
	if err := os.WriteFile(filepath.Join(f.root, "new-source"), []byte("updated during provider outage"), 0600); err != nil {
		t.Fatal(err)
	}
	clock.add(30 * time.Second)
	s = waitRetry(t, f, func(s Snapshot) bool { return s.Run.Phase == domain.WorkflowReady })
	freshTest := latestOperation(s.Operations, "tests", 1)
	freshReview := latestOperation(s.Operations, "review", 1)
	if freshTest.ID == oldTests.ID || freshTest.SnapshotID == oldTests.SnapshotID || freshReview.ID == oldReview.ID || freshReview.InputOperationID != freshTest.ID || !s.Current || s.Accepted {
		t.Fatalf("stale evidence reused: %+v", s)
	}
	if reviews.launches.Load() != 2 || reviews.checks.Load() != 1 || s.Run.ProviderRetry.Attempt != 1 || s.Run.ProviderRetry.State != "recovered" {
		t.Fatalf("duplicate recovery: %+v", s.Run.ProviderRetry)
	}
}

func TestProviderRetryStopAndSupersedeFenceDelayedProviderCallback(t *testing.T) {
	for _, supersede := range []bool{false, true} {
		t.Run(fmt.Sprint(supersede), func(t *testing.T) {
			f, clock, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
				return reportVerification(), nil
			})
			reviews := &recoveringReviews{scenarioReviews: f.reviews, entered: make(chan struct{}), release: make(chan struct{})}
			f.coordinator.deps.Reviews = reviews
			s := waitRetry(t, f, func(s Snapshot) bool { return s.Run.BlockCode == "provider_waiting" })
			clock.add(30 * time.Second)
			if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
				t.Fatal(err)
			}
			select {
			case <-reviews.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("probe not started")
			}
			s, _ = f.coordinator.Get(t.Context(), f.session.ID)
			if supersede {
				quiet, err := f.coordinator.supersede(t.Context(), *s.Run)
				if err != nil || quiet {
					t.Fatalf("supersede ignored active provider: quiet=%v err=%v", quiet, err)
				}
			} else {
				stopping, err := f.coordinator.Stop(t.Context(), f.session.ID, s.Run.Version)
				if err != nil || stopping.Run.Phase != domain.WorkflowStopping {
					t.Fatalf("Stop claimed premature quiescence: %+v %v", stopping.Run, err)
				}
			}
			close(reviews.release)
			deadline := time.Now().Add(5 * time.Second)
			for f.coordinator.isActive(s.Run.ID+":provider-recovery") && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			if supersede {
				s, _ = f.coordinator.Get(t.Context(), f.session.ID)
				if _, err := f.coordinator.supersede(t.Context(), *s.Run); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
					t.Fatal(err)
				}
			}
			s, _ = f.coordinator.Get(t.Context(), f.session.ID)
			if s.Run.Phase != domain.WorkflowStopped || !s.Run.StopRequested || s.Run.ProviderRetry.NextAt != nil || s.Accepted || reviews.launches.Load() != 1 {
				t.Fatalf("late callback revived stopped run: %+v", s.Run)
			}
		})
	}
}

func TestProviderRetryChangedConfigurationCannotConsumeOldAvailabilityProof(t *testing.T) {
	var plans atomic.Int32
	var plannedNewModel atomic.Bool
	f, clock, _ := retryFixture(t, func(_ context.Context, run domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		plans.Add(1)
		if run.ReviewerConfig.Model == "different-model" {
			plannedNewModel.Store(true)
		}
		return reportVerification(), nil
	})
	reviews := &recoveringReviews{scenarioReviews: f.reviews, entered: make(chan struct{}), release: make(chan struct{})}
	f.coordinator.deps.Reviews = reviews
	s := waitRetry(t, f, func(s Snapshot) bool { return s.Run.BlockCode == "provider_waiting" })
	clock.add(30 * time.Second)
	if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case <-reviews.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("probe not started")
	}
	project, _, err := f.store.GetProject(t.Context(), string(f.session.ProjectID))
	if err != nil {
		t.Fatal(err)
	}
	project.Config.Reviewers[0].AgentConfig.Model = "different-model"
	if err = f.store.UpsertProject(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	close(reviews.release)
	s = waitRetry(t, f, func(s Snapshot) bool { return s.Run.Phase == domain.WorkflowReady })
	if plans.Load() != 2 || !plannedNewModel.Load() || reviews.launches.Load() != 2 || !s.Current || s.Accepted || s.Run.ProviderRetry.NextAt != nil {
		t.Fatal("new configuration did not obtain a fresh plan, tests and review")
	}
}
