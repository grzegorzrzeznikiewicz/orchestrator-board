// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).
package localworkflow

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// AutomationState exposes preparation without leaking task text or delivery keys.
type AutomationState struct {
	Pending      bool   `json:"pending"`
	BlockMessage string `json:"blockMessage,omitempty"`
}

type intentStore interface {
	ListPendingLocalWorkflowIntents(context.Context) ([]domain.LocalWorkflowIntent, error)
	ConsumeLocalWorkflowIntent(context.Context, domain.SessionID, string, string, time.Time) (bool, error)
}

type transcriptStore interface {
	ConversationForSession(context.Context, domain.SessionID) (domain.ConversationRecord, error)
	LatestConversationTurn(context.Context, string) (domain.ConversationTurn, error)
	ConversationTurnOutput(context.Context, string, string) ([]domain.ConversationMessage, error)
}

func (c *Coordinator) automation(ctx context.Context, id domain.SessionID) (AutomationState, error) {
	store, ok := c.deps.Store.(intentStore)
	if !ok {
		return AutomationState{}, nil
	}
	pending, err := store.ListPendingLocalWorkflowIntents(ctx)
	for _, item := range pending {
		if item.SessionID == id {
			return AutomationState{Pending: true}, err
		}
	}
	return AutomationState{}, err
}

// ProcessAutomaticIntents consumes only durable new-task facts. No migration or
// startup sweep retrospectively starts work in an existing conversation.
func (c *Coordinator) ProcessAutomaticIntents(ctx context.Context) error {
	store, ok := c.deps.Store.(intentStore)
	if !ok {
		return nil
	}
	intents, err := store.ListPendingLocalWorkflowIntents(ctx)
	if err != nil {
		return err
	}
	// Coalesce directions queued together; the original brief remains in Task.
	latest := map[domain.SessionID]domain.LocalWorkflowIntent{}
	for _, intent := range intents {
		latest[intent.SessionID] = intent
	}
	for id, intent := range latest {
		session, found, err := c.deps.Store.GetSession(ctx, id)
		if err != nil {
			return err
		}
		if !found {
			continue
		}
		if session.IsTerminated {
			_, err = store.ConsumeLocalWorkflowIntent(ctx, id, intent.RequestID, "", c.now())
			if err != nil {
				return err
			}
			continue
		}
		if domain.NormalizeSessionMode(session.Mode) != domain.SessionModeChat && intent.TurnID == "" {
			continue
		}
		if session.Metadata.WorkspacePath == "" || session.Metadata.ControllerGeneration == "" && domain.NormalizeSessionMode(session.Mode) == domain.SessionModeChat || session.ProvisionState != "" && session.ProvisionState != domain.SessionProvisionReady {
			continue
		}
		old, found, err := c.deps.Store.CurrentLocalWorkflow(ctx, id)
		if err != nil {
			return err
		}
		if found && old.RequestID == intent.RequestID {
			if err = c.drain(ctx, id); err != nil {
				c.deps.Logger.Warn("drain pending automatic input", "session", id, "error", err)
				continue
			}
			for _, item := range intents {
				if item.SessionID == id {
					if _, err = store.ConsumeLocalWorkflowIntent(ctx, id, item.RequestID, old.ID, c.now()); err != nil {
						return err
					}
				}
			}
			continue
		}
		if found && old.ClosedAt == nil {
			quiet, err := c.supersede(ctx, old)
			if err != nil {
				return err
			}
			if !quiet {
				continue
			}
		}
		snap, err := c.startAutomatic(ctx, id, intent.RequestID, intent.Prompt)
		if err != nil {
			return err
		}
		if err = c.drain(ctx, id); err != nil {
			c.deps.Logger.Warn("drain automatic input", "session", id, "error", err)
			continue
		}
		for _, item := range intents {
			if item.SessionID == id {
				if _, err = store.ConsumeLocalWorkflowIntent(ctx, id, item.RequestID, snap.Run.ID, c.now()); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// supersede stops only the old workflow's owned effects, never a new human turn.
func (c *Coordinator) supersede(ctx context.Context, r domain.LocalWorkflowRun) (bool, error) {
	unlock := c.lock(r.ID)
	defer unlock()
	current, found, err := c.deps.Store.GetLocalWorkflow(ctx, r.ID)
	if err != nil || !found {
		return false, err
	}
	r = current
	if r.ClosedAt != nil {
		return true, nil
	}
	if r.BlockCode == "provider_execution_uncertain" || r.BlockCode == "provider_uncertain" || r.BlockCode == "review_uncertain" {
		return false, nil
	}
	r.StopRequested = true
	stopProviderRetry(&r, "exhausted")
	r.BlockCode = "superseded_waiting"
	r.BlockMessage = "Waiting for the previous verification to stop. If its process state is uncertain, confirm that the old command has ended before resuming."
	r.UpdatedAt = c.now()
	changed, err := c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, nil)
	if err != nil || !changed {
		return false, err
	}
	r.Version++
	ops, err := c.deps.Store.ListLocalWorkflowOperations(ctx, r.ID)
	if err != nil {
		return false, err
	}
	c.mu.Lock()
	for _, op := range ops {
		if cancel := c.active[op.ID]; cancel != nil {
			cancel()
		}
	}
	for _, key := range []string{r.ID + ":plan", r.ID + ":provider-recovery"} {
		if cancel := c.active[key]; cancel != nil {
			cancel()
		}
	}
	c.mu.Unlock()
	c.closeGuard(r.ID)
	r, quiet, err := c.quiesce(ctx, r, ops)
	if err != nil || !quiet {
		return false, err
	}
	at := c.now()
	r.ClosedAt = &at
	r.UpdatedAt = at
	r.Phase = domain.WorkflowStopped
	r.BlockCode = "superseded"
	r.BlockMessage = "Closed after a new user instruction. Earlier verification evidence remains in history."
	return c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, nil)
}

func (c *Coordinator) executorOutput(ctx context.Context, id domain.SessionID) (string, error) {
	store, ok := c.deps.Store.(transcriptStore)
	if !ok {
		return "", nil
	}
	conversation, err := store.ConversationForSession(ctx, id)
	if errors.Is(err, domain.ErrNoConversation) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	turn, err := store.LatestConversationTurn(ctx, conversation.ID)
	if errors.Is(err, domain.ErrNoConversationTurn) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	messages, err := store.ConversationTurnOutput(ctx, conversation.ID, turn.ID)
	if err != nil {
		return "", err
	}
	var output strings.Builder
	for _, message := range messages {
		if message.TurnID == turn.ID && message.Role == domain.MessageRoleAssistant && !message.Streaming {
			output.WriteString(message.Text)
			output.WriteString("\n")
		}
	}
	return output.String(), nil
}

func (c *Coordinator) beginPlanning(ctx context.Context, r domain.LocalWorkflowRun) error {
	if c.isActive(r.ID + ":plan") {
		return nil
	}
	if c.deps.Planner == nil {
		return c.block(ctx, r, "verification_plan", fmt.Errorf("verification planner is unavailable"))
	}
	var err error
	r, err = c.refreshProviderConfiguration(ctx, r)
	if err != nil {
		return c.block(ctx, r, "configuration", err)
	}
	contextID, err := c.providerContextID(ctx, r)
	if err != nil {
		return c.block(ctx, r, "configuration", err)
	}
	r.ExecutionOutput, err = c.executorOutput(ctx, r.SessionID)
	if err != nil {
		return c.block(ctx, r, "verification_plan", err)
	}
	r.PlanState = "running"
	r.UpdatedAt = c.now()
	changed, err := c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, nil)
	if err != nil || !changed {
		return err
	}
	r.Version++
	c.launch(r.ID+":plan", func(background context.Context) {
		planCtx, cancel := context.WithTimeout(background, 2*time.Minute)
		defer cancel()
		plan, planErr := c.deps.Planner.Plan(planCtx, r)
		if planErr == nil {
			planErr = plan.Validate()
		}
		if planErr == nil {
			planErr = validateScopedVerificationPlan(r, plan)
		}
		unlock := c.lock(r.ID)
		defer unlock()
		latest, found, err := c.deps.Store.GetLocalWorkflow(context.WithoutCancel(background), r.ID)
		if err != nil || !found || latest.ClosedAt != nil {
			return
		}
		settled := context.WithoutCancel(background)
		// Cancellation/version fences must not erase a failed provider teardown.
		if errors.Is(planErr, domain.ErrProviderExecutionUncertain) {
			stopProviderRetry(&latest, "uncertain")
			_ = c.block(settled, latest, "provider_execution_uncertain", fmt.Errorf("provider termination could not be confirmed; end the affected session before starting another task"))
			return
		}
		if latest.StopRequested || latest.Version != r.Version {
			// Persist only settlement after a version fence. The old result can
			// never replace a newer instruction, configuration or block reason.
			if latest.ProviderRetry != nil && latest.ProviderRetry.State == "checking" && (r.ProviderRetry == nil || latest.ProviderRetry.AttemptID != r.ProviderRetry.AttemptID) {
				return
			}
			if latest.PlanState != "running" {
				return
			}
			// Persist confirmed settlement even when Stop changed the version.
			// A failed checkpoint leaves the durable running claim fenced.
			latest.PlanState = "canceled"
			latest.UpdatedAt = c.now()
			if _, err = c.deps.Store.AdvanceLocalWorkflow(settled, latest.Version, latest, nil); err != nil {
				c.deps.Logger.Error("record canceled workflow planner", "error", err)
			}
			return
		}
		latest.PlanState = "failed" // The provider returned; later validation may still fail.
		currentID, contextErr := c.providerContextID(settled, latest)
		pending, pendingErr := c.deps.Store.HasPendingLocalReviewWork(settled, latest.SessionID)
		if contextErr != nil || currentID != contextID || pendingErr != nil || pending {
			_ = c.block(settled, latest, "worker_changed", fmt.Errorf("executor input or provider configuration changed during planning"))
			return
		}
		if planErr != nil {
			if next, safe := c.scheduleProviderRetry(latest, "planner", "", contextID, planErr); safe {
				_, _ = c.deps.Store.AdvanceLocalWorkflow(settled, latest.Version, next, nil)
				c.closeGuard(latest.ID)
			} else {
				_ = c.block(settled, latest, "verification_plan", planErr)
			}
			return
		}
		providerRecovered(&latest)
		latest.BlockCode, latest.BlockMessage = "", ""
		latest.VerificationPlan = &plan
		latest.TestCommands = append([]string{}, plan.Commands...)
		latest.PlanState = "complete"
		latest.UpdatedAt = c.now()
		if _, err = c.deps.Store.AdvanceLocalWorkflow(context.WithoutCancel(background), latest.Version, latest, nil); err != nil {
			c.deps.Logger.Error("record verification plan", "error", err)
		}
	})
	return nil
}

// Recovery of automatic work always obtains fresh verification evidence. Unknown
// old command delivery remains blocked rather than executing the command twice.
func (c *Coordinator) recoverAutomatic(ctx context.Context, r domain.LocalWorkflowRun, ops []domain.LocalWorkflowOperation) error {
	if r.StopRequested {
		return nil
	}
	var updated *domain.ProjectConfig
	var commands []string
	switch r.BlockCode {
	case "configuration", "verification_plan":
		session, found, err := c.deps.Store.GetSession(ctx, r.SessionID)
		if err != nil || !found {
			return err
		}
		project, found, err := c.deps.Store.GetProject(ctx, string(session.ProjectID))
		if err != nil || !found {
			return err
		}
		cfg := project.Config.WithDefaults()
		commands, err = effectiveConfiguredCommands(r, cfg)
		if err != nil {
			return c.block(ctx, r, "verification_scope", err)
		}
		if r.BlockCode == "verification_plan" && reflect.DeepEqual(r.TestCommands, commands) {
			return nil
		}
		if cfg.EffectiveReviewMode() != domain.ReviewTargetLocal || session.Harness != domain.HarnessCodex || len(cfg.Reviewers) != 1 || cfg.Reviewers[0].Harness != domain.ReviewerClaudeCode {
			return nil
		}
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("automatic project configuration: %w", err)
		}
		updated = &cfg
	case "restart", "workspace_changed", "worker_changed":
	default:
		return nil
	}
	next, quiet, err := c.quiesce(ctx, r, ops)
	if err != nil || !quiet {
		return err
	}
	r = next
	if updated != nil {
		r.TestCommands = commands
		r.Env = updated.Env
		r.ReviewerConfig = updated.Reviewers[0].AgentConfig
	}
	obs, err := c.deps.Worker.Observe(ctx, r.SessionID, "")
	if err != nil {
		return err
	}
	if obs.State == "queued" {
		return c.drain(ctx, r.SessionID)
	}
	if obs.State == "running" {
		return nil
	}
	if !workerVerificationAllowed(r, obs) {
		return nil
	}
	r.WorkerGeneration = obs.Generation
	r.Phase = domain.WorkflowWaiting
	r.BlockCode = ""
	r.BlockMessage = ""
	r.PlanState = "pending"
	r.VerificationPlan = nil
	r.UpdatedAt = c.now()
	_, err = c.deps.Store.AdvanceLocalWorkflow(ctx, r.Version, r, nil)
	return err
}

func (c *Coordinator) drain(ctx context.Context, id domain.SessionID) error {
	if drain, ok := c.deps.Worker.(interface {
		Drain(context.Context, domain.SessionID) error
	}); ok {
		return drain.Drain(ctx, id)
	}
	return nil
}
