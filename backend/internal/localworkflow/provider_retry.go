// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).

package localworkflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

const providerRetryLimit = 3

type workflowProviderChecker interface {
	CheckWorkflowProvider(context.Context, domain.LocalWorkflowRun) error
}

// providerContextID binds recovery to the exact current configuration without
// persisting credentials, environment values or model output in retry facts.
func (c *Coordinator) providerContextID(ctx context.Context, r domain.LocalWorkflowRun) (string, error) {
	s, found, err := c.deps.Store.GetSession(ctx, r.SessionID)
	if err != nil {
		return "", err
	}
	if !found || s.IsTerminated || s.Metadata.WorkspacePath != r.WorkspacePath {
		return "", fmt.Errorf("executor workspace is no longer available")
	}
	p, found, err := c.deps.Store.GetProject(ctx, string(s.ProjectID))
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("project configuration is no longer available")
	}
	cfg := p.Config.WithDefaults()
	if cfg.EffectiveReviewMode() != domain.ReviewTargetLocal || s.Harness != domain.HarnessCodex || len(cfg.Reviewers) != 1 || cfg.Reviewers[0].Harness != domain.ReviewerClaudeCode || cfg.Validate() != nil {
		return "", fmt.Errorf("local workflow provider configuration is invalid")
	}
	if !maps.Equal(r.Env, cfg.Env) || r.ReviewerConfig != cfg.Reviewers[0].AgentConfig {
		return "", fmt.Errorf("workflow provider configuration is stale")
	}
	requiredCommands, err := effectiveConfiguredCommands(r, cfg)
	if err != nil {
		return "", err
	}
	if r.VerificationPlan != nil && r.VerificationPlan.Profile != "report" {
		for _, required := range requiredCommands {
			if !slices.Contains(r.TestCommands, required) {
				return "", fmt.Errorf("configured verification checks changed; a fresh plan is required")
			}
		}
	}
	data, err := json.Marshal(struct {
		Config                               domain.ProjectConfig
		Harness                              domain.AgentHarness
		Model, Effort, Controller, Workspace string
		VerificationContract                 *domain.LocalWorkflowVerificationContract
	}{cfg, s.Harness, s.Metadata.Model, s.Metadata.Effort, s.Metadata.ControllerGeneration, r.WorkspacePath, r.VerificationContract})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// scheduleProviderRetry is pure until its caller atomically persists the run
// (and failed review operation, where applicable). Polls never spend a budget.
func (c *Coordinator) scheduleProviderRetry(r domain.LocalWorkflowRun, stage, operationID, contextID string, cause error) (domain.LocalWorkflowRun, bool) {
	reason, safe := domain.ProviderAccessFailure(cause)
	if !r.Automatic || !safe {
		return r, false
	}
	retry := domain.LocalWorkflowProviderRetry{MaxAttempts: providerRetryLimit}
	if r.ProviderRetry != nil {
		retry = *r.ProviderRetry
	}
	if retry.Attempt < 0 {
		return r, false
	}
	retry.MaxAttempts = providerRetryLimit
	retry.Stage, retry.Reason = stage, reason
	retry.ContextID, retry.OperationID = contextID, operationID
	retry.FailureAt = c.now()
	retry.AttemptID = ""
	r.Phase = domain.WorkflowBlocked
	if stage == "planner" {
		r.PlanState = "failed"
	}
	r.BlockCode = "provider_waiting"
	r.BlockMessage = "Waiting to retry provider access automatically."
	if retry.Attempt >= providerRetryLimit {
		retry.State, retry.NextAt = "exhausted", nil
		r.BlockCode = "provider_exhausted"
		r.BlockMessage = "Automatic provider attempts are exhausted. Restore provider access, then resume explicitly."
	} else {
		retry.State = "waiting"
		next := c.now().Add(30 * time.Second * time.Duration(1<<retry.Attempt))
		retry.NextAt = &next
	}
	r.ProviderRetry = &retry
	r.UpdatedAt = c.now()
	return r, true
}

func providerRecovered(r *domain.LocalWorkflowRun) {
	if r.ProviderRetry != nil {
		retry := *r.ProviderRetry
		retry.State, retry.NextAt, retry.AttemptID = "recovered", nil, ""
		r.ProviderRetry = &retry
	}
}

func stopProviderRetry(r *domain.LocalWorkflowRun, state string) {
	if r.ProviderRetry == nil {
		return
	}
	retry := *r.ProviderRetry
	if retry.State == "uncertain" && state == "exhausted" {
		state = "uncertain"
	}
	retry.State, retry.NextAt, retry.AttemptID = state, nil, ""
	r.ProviderRetry = &retry
}

func (c *Coordinator) providerRetryUncertain(ctx context.Context, r domain.LocalWorkflowRun) error {
	stopProviderRetry(&r, "uncertain")
	return c.block(ctx, r, "provider_uncertain", fmt.Errorf("previous provider attempt has no confirmed completion; end the affected session and confirm the process has ended before starting another task"))
}

// recoverProvider reserves an attempt before any provider I/O. Only one bounded
// asynchronous call runs; the normal daemon sweep merely checks durable times.
func (c *Coordinator) recoverProvider(ctx context.Context, r domain.LocalWorkflowRun, ops []domain.LocalWorkflowOperation) error {
	retry := r.ProviderRetry
	if retry == nil || r.StopRequested || r.ClosedAt != nil {
		return nil
	}
	if retry.State == "checking" {
		if c.isActive(r.ID+":plan") || c.isActive(r.ID+":provider-recovery") {
			return nil
		}
		return c.providerRetryUncertain(ctx, r)
	}
	if retry.State != "waiting" || retry.NextAt == nil || c.now().Before(*retry.NextAt) {
		return nil
	}
	if retry.Attempt < 0 || retry.Attempt >= providerRetryLimit || (retry.Stage != "planner" && retry.Stage != "review") {
		return c.providerRetryUncertain(ctx, r)
	}
	pending, err := c.deps.Store.HasPendingLocalReviewWork(ctx, r.SessionID)
	if err != nil || pending {
		return err
	}
	obs, err := c.deps.Worker.Observe(ctx, r.SessionID, "")
	if err != nil {
		return c.block(ctx, r, "executor_error", err)
	}
	if obs.Generation != r.WorkerGeneration {
		return c.block(ctx, r, "worker_changed", fmt.Errorf("executor controller changed"))
	}
	if !workerVerificationAllowed(r, obs) {
		return nil
	}
	contextID, err := c.providerContextID(ctx, r)
	if err != nil {
		return c.block(ctx, r, "configuration", err)
	}
	if retry.ContextID == "" || contextID != retry.ContextID {
		return c.block(ctx, r, "configuration", fmt.Errorf("provider configuration changed; fresh verification is required"))
	}
	var quiet bool
	r, quiet, err = c.quiesce(ctx, r, ops)
	if err != nil || !quiet {
		return err
	}
	reserved := *r.ProviderRetry
	reserved.Attempt++
	reserved.State, reserved.NextAt, reserved.AttemptID = "checking", nil, uuid.NewString()
	at := c.now()
	reserved.LastAt = &at
	r.ProviderRetry, r.UpdatedAt = &reserved, at
	changed, err := c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, nil)
	if err != nil || !changed {
		return err
	}
	r.Version++
	if reserved.Stage == "planner" {
		r.Phase, r.BlockCode, r.BlockMessage = domain.WorkflowWaiting, "", ""
		return c.beginPlanning(ctx, r)
	}
	checker, ok := c.deps.Reviews.(workflowProviderChecker)
	if !ok {
		return c.block(ctx, r, "configuration", fmt.Errorf("review provider validation is unavailable"))
	}
	c.launch(r.ID+":provider-recovery", func(background context.Context) {
		probe, cancel := context.WithTimeout(background, 30*time.Second)
		defer cancel()
		probeErr := checker.CheckWorkflowProvider(probe, r)
		unlock := c.lock(r.ID)
		defer unlock()
		settled := context.WithoutCancel(background)
		latest, found, err := c.deps.Store.GetLocalWorkflow(settled, r.ID)
		if err != nil || !found || latest.Version != r.Version || latest.StopRequested || latest.ClosedAt != nil || latest.ProviderRetry == nil || latest.ProviderRetry.AttemptID != reserved.AttemptID {
			return
		}
		if probeErr != nil {
			if next, safe := c.scheduleProviderRetry(latest, "review", reserved.OperationID, contextID, probeErr); safe {
				_, _ = c.deps.Store.AdvanceLocalWorkflow(settled, latest.Version, next, nil)
			} else {
				_ = c.block(settled, latest, "review_error", probeErr)
			}
			return
		}
		currentID, err := c.providerContextID(settled, latest)
		if err != nil || currentID != contextID {
			_ = c.block(settled, latest, "configuration", fmt.Errorf("provider configuration changed during validation"))
			return
		}
		pending, err := c.deps.Store.HasPendingLocalReviewWork(settled, latest.SessionID)
		if err != nil || pending {
			_ = c.block(settled, latest, "worker_changed", fmt.Errorf("new executor input arrived during provider validation"))
			return
		}
		providerRecovered(&latest)
		// Previous source observation was closed on failure. Start fresh tests;
		// a new review operation will consume only their new snapshot/guard.
		latest.Phase, latest.BlockCode, latest.BlockMessage = domain.WorkflowWaiting, "", ""
		latest.UpdatedAt = c.now()
		if _, err = c.deps.Store.AdvanceLocalWorkflow(settled, latest.Version, latest, nil); err != nil {
			c.deps.Logger.Error("record recovered review provider", "error", err)
		}
	})
	return nil
}

// refreshProviderConfiguration aligns actual request inputs with the configuration
// whose opaque identity will fence the request and any later recovery probe.
func (c *Coordinator) refreshProviderConfiguration(ctx context.Context, r domain.LocalWorkflowRun) (domain.LocalWorkflowRun, error) {
	s, found, err := c.deps.Store.GetSession(ctx, r.SessionID)
	if err != nil {
		return r, err
	}
	if !found || s.IsTerminated {
		return r, fmt.Errorf("executor is unavailable")
	}
	p, found, err := c.deps.Store.GetProject(ctx, string(s.ProjectID))
	if err != nil {
		return r, err
	}
	if !found {
		return r, fmt.Errorf("project is unavailable")
	}
	cfg := p.Config.WithDefaults()
	if cfg.EffectiveReviewMode() != domain.ReviewTargetLocal || s.Harness != domain.HarnessCodex || len(cfg.Reviewers) != 1 || cfg.Reviewers[0].Harness != domain.ReviewerClaudeCode || cfg.Validate() != nil {
		return r, fmt.Errorf("fix Local review, Codex and Claude provider configuration first")
	}
	r.TestCommands, err = effectiveConfiguredCommands(r, cfg)
	if err != nil {
		return r, err
	}
	r.Env = cfg.Env
	r.ReviewerConfig = cfg.Reviewers[0].AgentConfig
	return r, nil
}
