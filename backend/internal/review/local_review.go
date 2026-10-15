package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/localsnapshot"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type localReviewStore interface {
	InvalidateLocalReviewRun(context.Context, string, string, time.Time) error
}

// UsesLocalReview lets the transport service avoid all provider refresh work.
func (e *Engine) UsesLocalReview(ctx context.Context, id domain.SessionID) (bool, error) {
	worker, ok, err := e.sessions.GetSession(ctx, id)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("%w: worker session %q", ErrNotFound, id)
	}
	cfg, err := e.reviewProjectConfig(ctx, worker.ProjectID)
	return cfg.EffectiveReviewMode() == domain.ReviewTargetLocal, err
}

func (e *Engine) reviewProjectConfig(ctx context.Context, id domain.ProjectID) (domain.ProjectConfig, error) {
	if e.projects == nil {
		return domain.ProjectConfig{}, nil
	}
	p, ok, err := e.projects.GetProject(ctx, string(id))
	if err != nil {
		return domain.ProjectConfig{}, err
	}
	if !ok {
		return domain.ProjectConfig{}, nil
	}
	return p.Config.WithDefaults(), nil
}

func (e *Engine) triggerLocalLocked(ctx context.Context, worker domain.SessionRecord, cfg domain.ProjectConfig, opts TriggerOptions) (TriggerResult, error) {
	if opts.Source == domain.ReviewTriggerAuto {
		return TriggerResult{SkipReason: "local_workflow_requires_manual_trigger"}, nil
	}
	admission, ok := e.store.(ports.LocalReviewWorkspace)
	if !ok {
		return TriggerResult{}, fmt.Errorf("%w: local workspace admission unavailable", ErrInvalid)
	}
	releaseAdmission, err := admission.AcquireLocalReviewWorkspace(ctx, worker.ID)
	if err != nil {
		return TriggerResult{}, err
	}
	defer releaseAdmission()
	current, found, err := e.sessions.GetSession(ctx, worker.ID)
	if err != nil {
		return TriggerResult{}, err
	}
	if !found {
		return TriggerResult{}, ErrNotFound
	}
	worker = current
	pending, err := admission.HasPendingLocalReviewWork(ctx, worker.ID)
	if err != nil {
		return TriggerResult{}, err
	}
	if pending {
		return TriggerResult{}, fmt.Errorf("%w: executor has queued or running work", ErrConflict)
	}
	if worker.Activity.State != domain.ActivityIdle {
		return TriggerResult{}, fmt.Errorf("%w: Local review requires the executor to finish editing; worker is %q", ErrConflict, worker.Activity.State)
	}
	if opts.PRURL != "" {
		return TriggerResult{}, fmt.Errorf("%w: Local review does not accept a PR URL", ErrInvalid)
	}
	if e.localDataDir == "" {
		return TriggerResult{}, fmt.Errorf("%w: local review snapshot directory is not configured", ErrInvalid)
	}
	if _, ok := e.store.(localReviewStore); !ok {
		return TriggerResult{}, fmt.Errorf("%w: local review persistence is unavailable", ErrInvalid)
	}
	if err := opts.Config.Validate(); err != nil {
		return TriggerResult{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	harness, config, err := e.reviewerSelection(ctx, worker)
	if err != nil {
		return TriggerResult{}, err
	}
	if opts.Harness != "" {
		harness = opts.Harness
		config = opts.Config
	} else {
		config = mergeReviewerAgentConfig(config, opts.Config)
	}
	if !harness.IsKnown() {
		return TriggerResult{}, fmt.Errorf("%w: unknown reviewer %q", ErrInvalid, harness)
	}
	runs, err := e.store.ListReviewRunsBySession(ctx, worker.ID)
	if err != nil {
		return TriggerResult{}, err
	}
	for _, run := range runs {
		if run.TargetKind != domain.ReviewTargetLocal || run.InvalidatedAt != nil {
			continue
		}
		validErr := e.validateLocalRunLocked(ctx, run)
		if validErr != nil && !errors.Is(validErr, ErrConflict) {
			return TriggerResult{}, validErr
		}
		if validErr == nil && run.Harness == harness {
			if run.Status == domain.ReviewRunRunning || (!opts.Rerun && run.Verdict == domain.VerdictApproved && (run.Status == domain.ReviewRunComplete || run.Status == domain.ReviewRunDelivered)) {
				state, err := e.listLocalLocked(ctx, worker, harness)
				if err != nil {
					return TriggerResult{}, err
				}
				if opts.RejectReviewedHead {
					return TriggerResult{}, ErrHeadAlreadyReviewed
				}
				return TriggerResult{Run: run, Created: false, Reviews: state.Reviews, Runs: state.Runs, ReviewerHandleID: state.ReviewerHandleID, ReviewerSurface: state.ReviewerSurface}, nil
			}
		}
	}
	manifest, err := localsnapshot.Capture(ctx, worker.Metadata.WorkspacePath)
	if err != nil {
		return TriggerResult{}, fmt.Errorf("prepare local review: %w", err)
	}
	guard, err := localsnapshot.Watch(worker.Metadata.WorkspacePath, manifest)
	if err != nil {
		return TriggerResult{}, fmt.Errorf("observe local review: %w", err)
	}
	keepGuard := false
	defer func() {
		if !keepGuard {
			guard.Close()
		}
	}()
	snapshot, err := localsnapshot.Prepare(ctx, worker.Metadata.WorkspacePath, filepath.Join(e.localDataDir, "local-review"))
	if err != nil {
		return TriggerResult{}, err
	}
	keepSnapshot := false
	defer func() {
		if !keepSnapshot {
			_ = os.RemoveAll(snapshot.Path)
		}
	}()
	if err = guard.Check(ctx); err != nil {
		return TriggerResult{}, fmt.Errorf("%w: %w", ErrConflict, err)
	}
	latest, ok, err := e.sessions.GetSession(ctx, worker.ID)
	if err != nil {
		return TriggerResult{}, err
	}
	if !ok || latest.Activity.State != domain.ActivityIdle || !latest.Activity.LastActivityAt.Equal(worker.Activity.LastActivityAt) {
		return TriggerResult{}, fmt.Errorf("%w: executor resumed during snapshot preparation", ErrConflict)
	}
	row, _, err := e.store.GetReviewBySessionAndHarness(ctx, worker.ID, harness)
	if err != nil {
		return TriggerResult{}, err
	}
	if row.ReviewerHandleID != "" {
		if err := e.launcher.Destroy(ctx, row.ReviewerHandleID); err != nil {
			return TriggerResult{}, err
		}
	}
	now := e.clock()
	launchID := e.newID()
	row, err = e.upsertReview(ctx, worker, harness, "", "", launchID, domain.ActivityActive, now)
	if err != nil {
		return TriggerResult{}, err
	}
	row.IsArchived = false
	row.ReviewerActivityState = domain.ActivityActive
	mode := opts.InterfaceMode
	if mode == "" {
		mode = e.launcher.InterfaceMode(harness)
	}
	row.InterfaceMode = mode
	if err := e.store.UpsertReview(ctx, row); err != nil {
		return TriggerResult{}, err
	}
	source := opts.Source
	if source == "" {
		source = domain.ReviewTriggerManual
	}
	run := domain.ReviewRun{ID: e.newID(), ReviewID: row.ID, SessionID: worker.ID, BatchID: e.newID(), Harness: harness, TriggerSource: source, TargetKind: domain.ReviewTargetLocal, SnapshotID: snapshot.ID, SnapshotPath: snapshot.Path, SnapshotObservation: snapshot.ObservationID, SnapshotEpoch: e.localEpoch, Status: domain.ReviewRunRunning, CreatedAt: now}
	if err := e.store.InsertReviewRun(ctx, run); err != nil {
		return TriggerResult{}, err
	}
	keepSnapshot = true
	keepGuard = true
	e.trackLocalRun(run, guard)
	fail := func(cause error) (TriggerResult, error) {
		_, updateErr := e.store.UpdateReviewRunResult(ctx, run.ID, domain.ReviewRunFailed, domain.VerdictNone, cause.Error(), "", false)
		if updateErr != nil {
			return TriggerResult{}, fmt.Errorf("%w; record local failure: %w", cause, updateErr)
		}
		return TriggerResult{}, cause
	}
	release, err := e.beginHarnessUse(harness)
	if err != nil {
		return fail(err)
	}
	defer release()
	if err = e.launcher.Preflight(ctx, harness, snapshot.Path, mode); err != nil {
		return fail(err)
	}
	latest, ok, err = e.sessions.GetSession(ctx, worker.ID)
	if err != nil {
		return fail(err)
	}
	if !ok || latest.IsTerminated || latest.Activity.State != domain.ActivityIdle || !latest.Activity.LastActivityAt.Equal(worker.Activity.LastActivityAt) {
		return fail(fmt.Errorf("%w: executor resumed before reviewer launch", ErrConflict))
	}
	if err = guard.Check(ctx); err != nil {
		return fail(fmt.Errorf("%w: %w", ErrConflict, err))
	}
	spec := LaunchSpec{RunID: run.ID, BatchID: run.BatchID, ReviewSessionID: row.ID, LaunchID: launchID, WorkerID: worker.ID, ProjectID: worker.ProjectID, ProjectEnv: cfg.Env, Harness: harness, AgentConfig: config, WorkspacePath: snapshot.Path, TargetKind: domain.ReviewTargetLocal, SnapshotID: snapshot.ID, InterfaceMode: mode}
	launch, err := e.launcher.Spawn(ctx, spec)
	if err != nil {
		return fail(fmt.Errorf("launch local reviewer: %w", err))
	}
	row.ReviewerHandleID = launch.HandleID
	row.AgentSessionID = launch.AgentSessionID
	if launch.LaunchID != "" {
		row.ReviewerLaunchID = launch.LaunchID
	}
	if err := e.store.UpsertReview(ctx, row); err != nil {
		_ = e.launcher.Destroy(ctx, launch.HandleID)
		return fail(err)
	}
	state, err := e.listLocalLocked(ctx, worker, harness)
	if err != nil {
		return TriggerResult{}, err
	}
	return TriggerResult{Run: run, Created: true, CreatedRuns: []domain.ReviewRun{run}, Reviews: state.Reviews, Runs: state.Runs, ReviewerHandleID: legacyReviewerHandle(row), ReviewerSurface: reviewerSurface(row)}, nil
}

func (e *Engine) trackLocalRun(run domain.ReviewRun, guard *localsnapshot.Guard) {
	e.localMu.Lock()
	if e.localClosed {
		e.localMu.Unlock()
		guard.Close()
		return
	}
	e.localWG.Add(1)
	e.localGuards[run.ID] = guard
	e.localMu.Unlock()
	go func() {
		defer e.localWG.Done()
		defer func() { e.localMu.Lock(); delete(e.localGuards, run.ID); e.localMu.Unlock() }()
		select {
		case <-guard.Done():
			if !guard.Changed() {
				return
			}
		case <-guard.Changes():
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if store, ok := e.store.(localReviewStore); ok {
			_ = store.InvalidateLocalReviewRun(ctx, run.ID, "Workspace changed during or after local review", e.clock())
		}
		guard.Close()
	}()
}

// CloseLocalReviews releases native file watches when the daemon shuts down.
func (e *Engine) CloseLocalReviews() {
	e.localMu.Lock()
	e.localClosed = true
	guards := e.localGuards
	e.localGuards = make(map[string]*localsnapshot.Guard)
	e.localMu.Unlock()
	for _, guard := range guards {
		guard.Close()
	}
	e.localWG.Wait()
}

// ValidateLocalRun is the gate for accepting a local result or a later user
// acceptance claim. It re-reads durable facts and verifies current source bytes.
func (e *Engine) ValidateLocalRun(ctx context.Context, run domain.ReviewRun) error {
	if run.TargetKind != domain.ReviewTargetLocal {
		return nil
	}
	unlock := e.lockWorker(run.SessionID)
	defer unlock()
	return e.validateLocalRunLocked(ctx, run)
}

func (e *Engine) validateLocalRunLocked(ctx context.Context, run domain.ReviewRun) error {
	current, ok, err := e.store.GetReviewRun(ctx, run.ID)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: local review run is missing", ErrNotFound)
	}
	run = current
	if run.InvalidatedAt != nil {
		return fmt.Errorf("%w: local review is stale: %s", ErrConflict, run.InvalidationReason)
	}
	worker, ok, err := e.sessions.GetSession(ctx, run.SessionID)
	if err != nil {
		return err
	}
	reason := ""
	if !ok || worker.IsTerminated || worker.Activity.State != domain.ActivityIdle {
		reason = "Executor is editing or its idle state cannot be verified"
	}
	e.localMu.Lock()
	guard := e.localGuards[run.ID]
	e.localMu.Unlock()
	if reason == "" && (guard == nil || run.SnapshotEpoch != e.localEpoch) {
		reason = "Local review observation was interrupted by a restart"
	}
	if reason == "" && guard != nil {
		if err = guard.Check(ctx); err != nil {
			reason = "Workspace changed during or after local review"
		}
	}
	if reason == "" {
		manifest, captureErr := localsnapshot.Capture(ctx, worker.Metadata.WorkspacePath)
		if captureErr != nil {
			reason = "Workspace cannot be verified"
		} else if manifest.ID != run.SnapshotID || manifest.ObservationID != run.SnapshotObservation {
			reason = "Workspace changed since the local snapshot"

		}
	}
	if reason == "" {
		return nil
	}
	store, ok := e.store.(localReviewStore)
	if !ok {
		return fmt.Errorf("%w: local invalidation persistence is unavailable", ErrConflict)
	}
	if err := store.InvalidateLocalReviewRun(ctx, run.ID, reason, e.clock()); err != nil {
		return err
	}
	return fmt.Errorf("%w: %s", ErrConflict, reason)
}

func (e *Engine) listLocalLocked(ctx context.Context, worker domain.SessionRecord, harness domain.ReviewerHarness) (SessionReviews, error) {
	runs, err := e.store.ListReviewRunsBySession(ctx, worker.ID)
	if err != nil {
		return SessionReviews{}, err
	}
	for i := range runs {
		if runs[i].TargetKind != domain.ReviewTargetLocal {
			continue
		}
		validErr := e.validateLocalRunLocked(ctx, runs[i])
		if validErr != nil && !errors.Is(validErr, ErrConflict) {
			return SessionReviews{}, validErr
		}
		runs[i].Current = validErr == nil
		if validErr != nil {
			refreshed, ok, getErr := e.store.GetReviewRun(ctx, runs[i].ID)
			if getErr != nil {
				return SessionReviews{}, getErr
			}
			if ok {
				runs[i] = refreshed
			}
		}
	}
	state := PRReviewState{TargetKind: domain.ReviewTargetLocal, Title: "Local review", Status: ReviewStateNeedsReview}
	for i := range runs {
		run := &runs[i]
		if run.TargetKind != domain.ReviewTargetLocal {
			continue
		}
		if state.SnapshotID == "" {
			state.SnapshotID = run.SnapshotID
		}
		if run.Harness != harness {
			continue
		}
		if run.Current && state.LatestRun == nil {
			state.LatestRun = run
			state.SnapshotID = run.SnapshotID
			switch {
			case run.Status == domain.ReviewRunRunning:
				state.Status = ReviewStateRunning
			case (run.Status == domain.ReviewRunComplete || run.Status == domain.ReviewRunDelivered) && run.Verdict == domain.VerdictApproved:
				state.Status = ReviewStateUpToDate
			case (run.Status == domain.ReviewRunComplete || run.Status == domain.ReviewRunDelivered) && run.Verdict == domain.VerdictChangesRequested:
				state.Status = ReviewStateChangesRequested
			}
		}
		if !run.Current && state.PreviousRun == nil {
			state.PreviousRun = run
		}
	}
	row, _, err := e.store.GetReviewBySessionAndHarness(ctx, worker.ID, harness)
	if err != nil {
		return SessionReviews{}, err
	}
	if changed, reconcileErr := e.reconcileExitedReviewer(ctx, &row, runs); reconcileErr != nil {
		return SessionReviews{}, reconcileErr
	} else if changed {
		return e.listLocalLocked(ctx, worker, harness)
	}
	active := []domain.ReviewerSurface{}
	if reviewerRowActive(row, runs) {
		active = append(active, reviewerSurface(row))
	}
	return SessionReviews{ReviewerHandleID: legacyReviewerHandle(row), ReviewerHarness: harness, ReviewerActivityState: row.ReviewerActivityState, ReviewerSurface: reviewerSurface(row), ActiveReviewers: active, Reviews: []PRReviewState{state}, Runs: runs}, nil
}

func (e *Engine) localRestoreRequired(ctx context.Context, worker domain.SessionRecord) (bool, error) {
	cfg, err := e.reviewProjectConfig(ctx, worker.ProjectID)
	if err != nil || cfg.EffectiveReviewMode() != domain.ReviewTargetLocal {
		return false, err
	}
	runs, err := e.store.ListReviewRunsBySession(ctx, worker.ID)
	if err != nil {
		return true, err
	}
	for _, run := range runs {
		if run.TargetKind == domain.ReviewTargetLocal {
			if err = e.validateLocalRunLocked(ctx, run); err != nil && !errors.Is(err, ErrConflict) {
				return true, err
			}
		}
	}
	return true, nil
}
