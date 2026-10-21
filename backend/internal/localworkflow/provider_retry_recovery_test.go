// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package localworkflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

func replaceProviderCoordinator(t *testing.T, f *workflowFixture) *Coordinator {
	t.Helper()
	previous := f.coordinator
	if err := previous.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	replacement := New(previous.deps)
	t.Cleanup(func() { _ = replacement.Close(context.Background()) })
	f.coordinator = replacement
	return replacement
}

func awaitProviderPlannerExit(t *testing.T, c *Coordinator, runID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for c.isActive(runID + ":plan") {
		if time.Now().After(deadline) {
			t.Fatal("planner did not leave its owned launch slot")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestProviderRecoveryInitialPlanningCrashCannotReplay(t *testing.T) {
	for _, stopping := range []bool{false, true} {
		t.Run(fmt.Sprintf("stop_requested_%t", stopping), func(t *testing.T) {
			f := setupWorkflow(t, nil, 3)
			var calls atomic.Int32
			f.coordinator.deps.Planner = plannerFunc(func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
				calls.Add(1)
				return reportVerification(), nil
			})
			s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "initial-planner-crash", "Produce a report")
			if err != nil {
				t.Fatal(err)
			}
			// A persisted claim survives the daemon. There is no native provider
			// completion with which a replacement coordinator could reconcile it.
			r := *s.Run
			r.PlanState = "running"
			r.StopRequested = stopping
			if stopping {
				r.Phase = domain.WorkflowStopping
			}
			if ok, err := f.store.AdvanceLocalWorkflow(t.Context(), r.Version, r, nil); err != nil || !ok {
				t.Fatalf("persist planning claim: %v, %t", err, ok)
			}
			c := replaceProviderCoordinator(t, f)
			if err := c.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			for range 5 {
				if err := c.Evaluate(t.Context(), r.ID); err != nil {
					t.Fatal(err)
				}
			}
			s, err = c.Get(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 0 || s.Run.BlockCode != "provider_execution_uncertain" || s.Run.Phase != domain.WorkflowBlocked || s.Current || s.Accepted || s.Run.StopRequested != stopping {
				t.Fatalf("unconfirmed planning claim was replayed or presented as settled: calls=%d run=%+v", calls.Load(), s.Run)
			}
			if _, err := c.Resume(t.Context(), f.session.ID, s.Run.Version, nil); err == nil {
				t.Fatal("manual Resume bypassed unconfirmed initial provider execution")
			}
		})
	}
}

func TestProviderRecoveryCheckingCrashCannotBeClearedByStopOrResume(t *testing.T) {
	for _, stage := range []string{"planner", "review"} {
		for _, stopping := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s_stop_requested_%t", stage, stopping), func(t *testing.T) {
				var calls atomic.Int32
				f, clock, _ := retryFixture(t, func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
					calls.Add(1)
					return domain.LocalVerificationPlan{}, transientProvider()
				})
				s := waitRetry(t, f, func(s Snapshot) bool { return s.Run.ProviderRetry != nil })
				awaitProviderPlannerExit(t, f.coordinator, s.Run.ID)
				r := *s.Run
				retry := *r.ProviderRetry
				retry.Stage, retry.State = stage, "checking"
				retry.Attempt, retry.AttemptID, retry.NextAt = 1, "crashed-provider-attempt", nil
				r.ProviderRetry, r.StopRequested = &retry, stopping
				if stopping {
					r.Phase = domain.WorkflowStopping
				}
				if ok, err := f.store.AdvanceLocalWorkflow(t.Context(), r.Version, r, nil); err != nil || !ok {
					t.Fatalf("persist checking claim: %v, %t", err, ok)
				}
				c := replaceProviderCoordinator(t, f)
				if err := c.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
				clock.add(time.Hour)
				for range 5 {
					if err := c.Evaluate(t.Context(), r.ID); err != nil {
						t.Fatal(err)
					}
				}
				s, err := c.Get(t.Context(), f.session.ID)
				if err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 1 || s.Run.ProviderRetry.State != "uncertain" || s.Run.ProviderRetry.Attempt != 1 || s.Run.ProviderRetry.NextAt != nil || s.Run.Phase != domain.WorkflowBlocked || s.Current || s.Accepted {
					t.Fatalf("interrupted retry was replayed or presented as settled: calls=%d run=%+v", calls.Load(), s.Run)
				}
				if _, err := c.Resume(t.Context(), f.session.ID, s.Run.Version, nil); err == nil {
					t.Fatal("manual Resume cleared an unreconciled provider attempt")
				}
				s, err = c.Stop(t.Context(), f.session.ID, s.Run.Version)
				if err != nil {
					t.Fatal(err)
				}
				if s.Run.Phase == domain.WorkflowStopped || s.Run.ProviderRetry.State != "uncertain" || !s.Run.StopRequested {
					t.Fatalf("Stop erased provider uncertainty: %+v", s.Run)
				}
				// Persisted uncertainty must still fence a second daemon, even
				// after Stop has removed all of the first daemon's in-memory work.
				c = replaceProviderCoordinator(t, f)
				if err := c.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
				s, err = c.Get(t.Context(), f.session.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := c.Resume(t.Context(), f.session.ID, s.Run.Version, nil); err == nil {
					t.Fatal("Stop followed by daemon restart permitted an unreconciled provider replay")
				}
			})
		}
	}
}

type failProviderSettlementStore struct {
	Store
	failed atomic.Bool
	cause  error
}

func (s *failProviderSettlementStore) AdvanceLocalWorkflow(ctx context.Context, version int64, r domain.LocalWorkflowRun, op *domain.LocalWorkflowOperation) (bool, error) {
	if r.Phase == domain.WorkflowBlocked && strings.HasPrefix(r.BlockCode, "provider_") && s.failed.CompareAndSwap(false, true) {
		return false, s.cause
	}
	return s.Store.AdvanceLocalWorkflow(ctx, version, r, op)
}

func TestProviderRecoveryFailedSettlementCannotReplayPlanning(t *testing.T) {
	for _, uncertain := range []bool{false, true} {
		for _, storageError := range []bool{false, true} {
			t.Run(fmt.Sprintf("uncertain_%t_storage_error_%t", uncertain, storageError), func(t *testing.T) {
				f := setupWorkflow(t, nil, 3)
				store := &failProviderSettlementStore{Store: f.store}
				if storageError {
					store.cause = errors.New("injected provider settlement write failure")
				}
				f.coordinator.deps.Store = store
				var calls atomic.Int32
				f.coordinator.deps.Planner = plannerFunc(func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
					calls.Add(1)
					cause := transientProvider()
					if uncertain {
						cause = errors.Join(cause, domain.ErrProviderExecutionUncertain)
					}
					return domain.LocalVerificationPlan{}, cause
				})
				s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "settlement-storage-failure", "Produce a report")
				if err != nil {
					t.Fatal(err)
				}
				if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
					t.Fatal(err)
				}
				awaitProviderPlannerExit(t, f.coordinator, s.Run.ID)
				if !store.failed.Load() {
					t.Fatal("the callback settlement write was not fault-injected")
				}
				for range 5 {
					if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
						t.Fatal(err)
					}
				}
				s, err = f.coordinator.Get(t.Context(), f.session.ID)
				if err != nil {
					t.Fatal(err)
				}
				if calls.Load() != 1 || s.Run.Phase != domain.WorkflowBlocked || s.Run.BlockCode != "provider_execution_uncertain" || s.Current || s.Accepted {
					t.Fatalf("lost callback checkpoint permitted replay or false completion: calls=%d run=%+v", calls.Load(), s.Run)
				}
				c := replaceProviderCoordinator(t, f)
				if err := c.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
				s, err = c.Get(t.Context(), f.session.ID)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := c.Resume(t.Context(), f.session.ID, s.Run.Version, nil); err == nil {
					t.Fatal("restart cleared a failed provider settlement checkpoint")
				}
			})
		}
	}
}

type failReviewProviderSettlementStore struct {
	Store
	failed atomic.Bool
	cause  error
}

func TestProviderRecoveryFailedPlannerSettlementCannotBeStoppedBeforePoll(t *testing.T) {
	for _, storageError := range []bool{false, true} {
		t.Run(fmt.Sprintf("storage_error_%t", storageError), func(t *testing.T) {
			f := setupWorkflow(t, nil, 3)
			store := &failProviderSettlementStore{Store: f.store}
			if storageError {
				store.cause = errors.New("injected uncertain planner settlement failure")
			}
			f.coordinator.deps.Store = store
			var calls atomic.Int32
			f.coordinator.deps.Planner = plannerFunc(func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
				calls.Add(1)
				return domain.LocalVerificationPlan{}, errors.Join(transientProvider(), domain.ErrProviderExecutionUncertain)
			})
			s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "stop-before-lost-planner-settlement-poll", "Produce a report")
			if err != nil {
				t.Fatal(err)
			}
			if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
				t.Fatal(err)
			}
			awaitProviderPlannerExit(t, f.coordinator, s.Run.ID)
			if !store.failed.Load() {
				t.Fatal("uncertain planner callback settlement was not fault-injected")
			}
			s, err = f.coordinator.Get(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			// No Evaluate runs between the lost callback and the human's Stop.
			s, err = f.coordinator.Stop(t.Context(), f.session.ID, s.Run.Version)
			if err != nil {
				t.Fatal(err)
			}
			if s.Run.Phase == domain.WorkflowStopped || calls.Load() != 1 || s.Current || s.Accepted {
				t.Fatalf("Stop converted a lost uncertain planner result to a settled run: %+v", s.Run)
			}
			c := replaceProviderCoordinator(t, f)
			if err := c.Recover(t.Context()); err != nil {
				t.Fatal(err)
			}
			s, err = c.Get(t.Context(), f.session.ID)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := c.Resume(t.Context(), f.session.ID, s.Run.Version, nil); err == nil {
				t.Fatal("immediate Stop and restart permitted an uncertain planner replay")
			}
		})
	}
}

func (s *failReviewProviderSettlementStore) CompleteLocalWorkflowOperation(ctx context.Context, version int64, op domain.LocalWorkflowOperation, r domain.LocalWorkflowRun, results []domain.LocalWorkflowTestResult) (bool, error) {
	if op.Kind == "review" && op.State == "uncertain" && s.failed.CompareAndSwap(false, true) {
		return false, s.cause
	}
	return s.Store.CompleteLocalWorkflowOperation(ctx, version, op, r, results)
}

type uncertainFailedProviderReviews struct {
	*scenarioReviews
	launches atomic.Int32
}

func (r *uncertainFailedProviderReviews) TriggerWorkflowLocal(_ context.Context, run domain.LocalWorkflowRun, id, snapshot string) (domain.ReviewRun, error) {
	r.launches.Add(1)
	// A launch may fail after creating a provider process. A failed review row
	// records its outcome without supplying proof that this process has ended.
	result := domain.ReviewRun{ID: id, SessionID: run.SessionID, SnapshotID: snapshot, TargetKind: domain.ReviewTargetLocal, Status: domain.ReviewRunFailed}
	r.mu.Lock()
	r.runs[id] = result
	r.mu.Unlock()
	return result, errors.Join(transientProvider(), domain.ErrProviderExecutionUncertain)
}

func TestProviderRecoveryFailedReviewSettlementCannotBeCleared(t *testing.T) {
	for _, stopFirst := range []bool{false, true} {
		for _, storageError := range []bool{false, true} {
			t.Run(fmt.Sprintf("stop_before_poll_%t_storage_error_%t", stopFirst, storageError), func(t *testing.T) {
				f := setupWorkflow(t, nil, 3)
				store := &failReviewProviderSettlementStore{Store: f.store}
				if storageError {
					store.cause = errors.New("injected reviewer settlement write failure")
				}
				f.coordinator.deps.Store = store
				reviews := &uncertainFailedProviderReviews{scenarioReviews: f.reviews}
				f.coordinator.deps.Reviews = reviews
				f.coordinator.deps.Planner = plannerFunc(func(context.Context, domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
					return reportVerification(), nil
				})
				s, err := f.coordinator.startAutomatic(t.Context(), f.session.ID, "review-settlement-storage-failure", "Produce a report")
				if err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(5 * time.Second)
				for !store.failed.Load() {
					if time.Now().After(deadline) {
						t.Fatal("reviewer callback settlement was not fault-injected")
					}
					if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
						t.Fatal(err)
					}
					time.Sleep(time.Millisecond)
				}
				ops, err := f.store.ListLocalWorkflowOperations(t.Context(), s.Run.ID)
				if err != nil {
					t.Fatal(err)
				}
				op := latestOperation(ops, "review", s.Run.Cycle)
				if op == nil || op.ReviewRunID != "" || op.State != "reserved" {
					t.Fatalf("unexpected durable reservation after callback write failure: %+v", op)
				}
				for f.coordinator.isActive(op.ID) {
					if time.Now().After(deadline) {
						t.Fatal("review callback did not release its launch slot")
					}
					time.Sleep(time.Millisecond)
				}
				if stopFirst {
					s, err = f.coordinator.Get(t.Context(), f.session.ID)
					if err != nil {
						t.Fatal(err)
					}
					s, err = f.coordinator.Stop(t.Context(), f.session.ID, s.Run.Version)
					if err != nil {
						t.Fatal(err)
					}
					if s.Run.Phase == domain.WorkflowStopped {
						t.Fatal("Stop treated failed review status as proof the unconfirmed process ended")
					}
				} else if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
					t.Fatal(err)
				}
				c := replaceProviderCoordinator(t, f)
				if err := c.Recover(t.Context()); err != nil {
					t.Fatal(err)
				}
				s, err = c.Get(t.Context(), f.session.ID)
				if err != nil {
					t.Fatal(err)
				}
				if s.Current || s.Accepted || reviews.launches.Load() != 1 {
					t.Fatalf("uncertain review produced evidence or a duplicate launch: %+v", s)
				}
				if _, err := c.Resume(t.Context(), f.session.ID, s.Run.Version, nil); err == nil {
					t.Fatal("failed reviewer settlement permitted replay after restart")
				}
			})
		}
	}
}

func TestProviderRecoverySupersededPlannerCannotRestartAfterSettledCancellation(t *testing.T) {
	var calls atomic.Int32
	entered := make(chan struct{})
	f, _, _ := retryFixture(t, func(ctx context.Context, _ domain.LocalWorkflowRun) (domain.LocalVerificationPlan, error) {
		if calls.Add(1) == 1 {
			close(entered)
			<-ctx.Done()
			return domain.LocalVerificationPlan{}, ctx.Err()
		}
		return reportVerification(), nil
	})
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("planner did not start")
	}
	s, err := f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	closed, err := f.coordinator.supersede(t.Context(), *s.Run)
	if err != nil || closed {
		t.Fatalf("supersede did not wait for the active planner: closed=%t error=%v", closed, err)
	}
	awaitProviderPlannerExit(t, f.coordinator, s.Run.ID)
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Run.StopRequested || s.Run.PlanState != "canceled" || s.Run.ClosedAt != nil {
		t.Fatalf("settled cancellation checkpoint was not retained: %+v", s.Run)
	}
	for range 5 {
		if err := f.coordinator.Evaluate(t.Context(), s.Run.ID); err != nil {
			t.Fatal(err)
		}
		awaitProviderPlannerExit(t, f.coordinator, s.Run.ID)
	}
	if calls.Load() != 1 {
		t.Fatalf("Evaluate restarted the superseded planner %d times", calls.Load()-1)
	}
	s, err = f.coordinator.Get(t.Context(), f.session.ID)
	if err != nil {
		t.Fatal(err)
	}
	closed, err = f.coordinator.supersede(t.Context(), *s.Run)
	if err != nil || !closed {
		t.Fatalf("confirmed cancellation prevented normal superseding: closed=%t error=%v", closed, err)
	}
}
