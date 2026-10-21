// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package localworkflow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type retryConfigReviews struct {
	*scenarioReviews
	seen chan domain.LocalWorkflowRun
}

func (r *retryConfigReviews) TriggerWorkflowLocal(ctx context.Context, run domain.LocalWorkflowRun, id, snapshot string) (domain.ReviewRun, error) {
	r.seen <- run
	return r.scenarioReviews.TriggerWorkflowLocal(ctx, run, id, snapshot)
}

func TestProviderRetryManualResumeUsesFreshExecutionConfig(t *testing.T) {
	const oldCommand = `test "$RETRY_CONTEXT" = old`
	const newCommand = `test "$RETRY_CONTEXT" = current`
	f := setupWorkflow(t, []string{oldCommand}, 3)
	clock := &retryClock{at: time.Now().UTC()}
	f.coordinator.deps.Clock = clock.now
	project, _, err := f.store.GetProject(t.Context(), "project")
	if err != nil {
		t.Fatal(err)
	}
	project.Config.Env = map[string]string{"RETRY_CONTEXT": "old"}
	project.Config.Reviewers[0].AgentConfig.Model = "old-review-model"
	if err = f.store.UpsertProject(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	planned := make(chan domain.LocalWorkflowRun, 1)
	f.coordinator.deps.Planner = plannerFunc(func(_ context.Context, run domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		if calls.Add(1) <= 4 {
			return domain.LocalVerificationPlan{}, transientProvider()
		}
		planned <- run
		return domain.LocalVerificationPlan{Profile: "implementation", Rationale: "Check code", Commands: append([]string{}, run.TestCommands...), Checks: []string{"Configured regression passes"}}, nil
	})
	reviews := &retryConfigReviews{scenarioReviews: f.reviews, seen: make(chan domain.LocalWorkflowRun, 1)}
	f.coordinator.deps.Reviews = reviews
	if _, err = f.coordinator.startAutomatic(t.Context(), f.session.ID, "retry-config", "Implement and verify the correction"); err != nil {
		t.Fatal(err)
	}
	waitRetry(t, f, func(s Snapshot) bool { return s.Run.BlockCode == "provider_waiting" })
	var s Snapshot
	for attempt, delay := range []time.Duration{30 * time.Second, 60 * time.Second, 120 * time.Second} {
		clock.add(delay)
		want := attempt + 1
		s = waitRetry(t, f, func(s Snapshot) bool {
			return s.Run.ProviderRetry != nil && s.Run.ProviderRetry.Attempt == want && s.Run.ProviderRetry.State != "checking"
		})
	}
	if s.Run.BlockCode != "provider_exhausted" || calls.Load() != 4 {
		t.Fatalf("not exhausted: %+v", s.Run)
	}
	project.Config.Env = map[string]string{"RETRY_CONTEXT": "current"}
	project.Config.TestCommands = []string{newCommand}
	project.Config.Reviewers[0].AgentConfig.Model = "fresh-review-model"
	if err = f.store.UpsertProject(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	s, err = f.coordinator.Resume(t.Context(), f.session.ID, s.Run.Version, nil)
	if err != nil {
		t.Fatal(err)
	}
	if s.Run.ProviderRetry != nil {
		t.Fatal("explicit Resume did not reset the exhausted budget")
	}
	s = driveAutomaticReady(t, f, s.Run.ID)
	for label, got := range map[string]domain.LocalWorkflowRun{"planner": <-planned, "review": <-reviews.seen, "settled run": *s.Run} {
		if !reflect.DeepEqual(got.Env, project.Config.Env) || got.ReviewerConfig != project.Config.Reviewers[0].AgentConfig || !reflect.DeepEqual(got.TestCommands, project.Config.TestCommands) {
			t.Errorf("%s used stale project execution config: env=%v reviewer=%+v commands=%v", label, got.Env, got.ReviewerConfig, got.TestCommands)
		}
	}
	if len(s.Tests) != 1 || s.Tests[0].Command != newCommand || s.Tests[0].Status != "succeeded" || !s.Current || s.Accepted || len(s.Decisions) != 0 {
		t.Fatalf("fresh implementation evidence/human gate missing: %+v", s)
	}
}

type delayedUncertainRetryReviews struct {
	*scenarioReviews
	entered, release chan struct{}
}

func (r *delayedUncertainRetryReviews) TriggerWorkflowLocal(_ context.Context, _ domain.LocalWorkflowRun, id, _ string) (domain.ReviewRun, error) {
	close(r.entered)
	<-r.release
	return domain.ReviewRun{ID: id, Status: domain.ReviewRunFailed}, errors.Join(transientProvider(), domain.ErrProviderExecutionUncertain)
}

func TestProviderRetryStopRetainsDelayedReviewTerminationUncertainty(t *testing.T) {
	f, _, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		return reportVerification(), nil
	})
	reviews := &delayedUncertainRetryReviews{scenarioReviews: f.reviews, entered: make(chan struct{}), release: make(chan struct{})}
	f.coordinator.deps.Reviews = reviews
	released := false
	t.Cleanup(func() {
		if !released {
			close(reviews.release)
		}
	})
	waitRetry(t, f, func(s Snapshot) bool { return s.Run.Phase == domain.WorkflowReviewing })
	select {
	case <-reviews.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("review launch did not start")
	}
	s, _ := f.coordinator.Get(t.Context(), f.session.ID)
	stopping, err := f.coordinator.Stop(t.Context(), f.session.ID, s.Run.Version)
	if err != nil || stopping.Run.Phase != domain.WorkflowStopping {
		t.Fatalf("premature Stop: %+v %v", stopping.Run, err)
	}
	close(reviews.release)
	released = true
	op := latestOperation(s.Operations, "review", s.Run.Cycle)
	if op == nil {
		t.Fatal("missing review operation")
	}
	deadline := time.Now().Add(5 * time.Second)
	for f.coordinator.isActive(op.ID) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if err = f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
		t.Fatal(err)
	}
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	op = latestOperation(s.Operations, "review", s.Run.Cycle)
	if s.Run.Phase != domain.WorkflowBlocked || !s.Run.StopRequested || op == nil || op.State != "uncertain" || op.CompletedAt != nil || s.Current || s.Accepted {
		t.Fatalf("Stop erased unconfirmed review termination: run=%+v operation=%+v", s.Run, op)
	}
	if _, err = f.coordinator.Resume(t.Context(), f.session.ID, s.Run.Version, nil); err == nil {
		t.Fatal("manual Resume replayed a review with unknown termination")
	}
}

func TestProviderRetryChangedMandatoryChecksRequireFreshPlanAndTests(t *testing.T) {
	const configured = "test -d ."
	const discovered = "test -f value"
	const added = "test -r value"
	f := setupWorkflow(t, []string{configured}, 3)
	if err := os.WriteFile(filepath.Join(f.root, "value"), []byte("correct"), 0600); err != nil {
		t.Fatal(err)
	}
	var plans atomic.Int32
	f.coordinator.deps.Planner = plannerFunc(func(_ context.Context, run domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		plans.Add(1)
		commands := append(append([]string{}, run.TestCommands...), discovered)
		return domain.LocalVerificationPlan{Profile: "implementation", Rationale: "Code changes", Commands: commands, Checks: []string{"Configured and discovered checks pass"}}, nil
	})
	reviews := &retryConfigReviews{scenarioReviews: f.reviews, seen: make(chan domain.LocalWorkflowRun, 2)}
	f.coordinator.deps.Reviews = reviews
	if _, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "checks-changed", "Implement and verify"); err != nil {
		t.Fatal(err)
	}
	s := waitRetry(t, f, func(s Snapshot) bool { return latestOperation(s.Operations, "tests", 0) != nil })
	// Stop advancing once tests are reserved. Snapshot reads span multiple queries,
	// so a stale testing phase must not accidentally drive beginReview before the
	// fixture changes configuration at this exact boundary.
	deadline := time.Now().Add(5 * time.Second)
	var err error
	for time.Now().Before(deadline) {
		s, err = f.coordinator.Get(t.Context(), f.session.ID)
		if err != nil {
			t.Fatal(err)
		}
		if s.Run.Phase == domain.WorkflowReviewing {
			break
		}
		time.Sleep(time.Millisecond)
	}
	oldTests := latestOperation(s.Operations, "tests", s.Run.Cycle)
	if oldTests == nil || oldTests.State != "succeeded" || len(s.Tests) != 2 {
		t.Fatalf("initial configured plus discovered tests did not pass: %+v", s.Operations)
	}
	project, _, err := f.store.GetProject(t.Context(), "project")
	if err != nil {
		t.Fatal(err)
	}
	project.Config.TestCommands = []string{configured, added}
	if err = f.store.UpsertProject(t.Context(), project); err != nil {
		t.Fatal(err)
	}
	if err = f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
		t.Fatal(err)
	}
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s.Run.Phase != domain.WorkflowBlocked || s.Run.BlockCode != "configuration" || latestOperation(s.Operations, "review", 0) != nil || len(reviews.seen) != 0 {
		t.Fatalf("new mandatory check was bypassed by stale plan: run=%+v operations=%+v", s.Run, s.Operations)
	}
	s = driveAutomaticReady(t, f, s.Run.ID)
	freshTests := latestOperation(s.Operations, "tests", s.Run.Cycle)
	if freshTests == nil || freshTests.ID == oldTests.ID || freshTests.State != "succeeded" || plans.Load() != 2 || len(reviews.seen) != 1 || !s.Current || s.Accepted || !slices.Contains(s.Run.TestCommands, discovered) || !slices.Contains(s.Run.TestCommands, added) {
		t.Fatalf("fresh complete verification missing: run=%+v operations=%+v", s.Run, s.Operations)
	}
	var freshCommands []string
	for _, result := range s.Tests {
		if result.OperationID == freshTests.ID && result.Status == "succeeded" {
			freshCommands = append(freshCommands, result.Command)
		}
	}
	if !slices.Contains(freshCommands, configured) || !slices.Contains(freshCommands, added) || !slices.Contains(freshCommands, discovered) {
		t.Fatalf("fresh tests omitted checks: %v", freshCommands)
	}
}

func TestProviderRetrySupersedeCannotErasePlannerTerminationUncertainty(t *testing.T) {
	var calls atomic.Int32
	f, _, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		calls.Add(1)
		return domain.LocalVerificationPlan{}, errors.Join(transientProvider(), domain.ErrProviderExecutionUncertain)
	})
	s := waitRetry(t, f, func(s Snapshot) bool { return s.Run.BlockCode == "provider_execution_uncertain" })
	quiet, err := f.coordinator.supersede(t.Context(), *s.Run)
	if err != nil {
		t.Fatal(err)
	}
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if quiet || s.Run.ClosedAt != nil || s.Run.BlockCode != "provider_execution_uncertain" || s.Run.Phase == domain.WorkflowStopped || calls.Load() != 1 {
		t.Fatalf("supersede erased durable teardown uncertainty: quiet=%v run=%+v", quiet, s.Run)
	}
}
