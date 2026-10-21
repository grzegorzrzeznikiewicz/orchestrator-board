// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package review

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"

	"github.com/aoagents/agent-orchestrator/backend/internal/agentlaunch"
	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type workflowProviderLauncher interface {
	CheckWorkflowProvider(context.Context, LaunchSpec) error
}

type workflowPreflightLauncher interface {
	PreflightWorkflow(context.Context, LaunchSpec) error
}

func preflightWorkflow(ctx context.Context, launcher Launcher, spec LaunchSpec) error {
	if preflight, ok := launcher.(workflowPreflightLauncher); ok {
		return preflight.PreflightWorkflow(ctx, spec)
	}
	if check, ok := launcher.(workflowProviderLauncher); ok {
		if err := blockingProviderFailure(ctx, check.CheckWorkflowProvider(ctx, spec)); err != nil {
			return err
		}
	}
	return launcher.Preflight(ctx, spec.Harness, spec.WorkspacePath, spec.InterfaceMode)
}

// PreflightWorkflow retains fresh exact-context proof through the remaining
// binary and compatibility checks. An unrelated global auth cache cannot reject
// a freshly accepted project credential. Unknown providers retain native launch.
func (l *agentLauncher) PreflightWorkflow(ctx context.Context, spec LaunchSpec) error {
	err := l.CheckWorkflowProvider(ctx, spec)
	if err == nil {
		return l.preflight(ctx, spec.Harness, spec.WorkspacePath, true, spec.InterfaceMode)
	}
	if err := blockingProviderFailure(ctx, err); err != nil {
		return err
	}
	return l.Preflight(ctx, spec.Harness, spec.WorkspacePath, spec.InterfaceMode)
}

func blockingProviderFailure(ctx context.Context, err error) error {
	err = prelaunchProviderFailure(ctx, err)
	_, safe := domain.ProviderAccessFailure(err)
	if safe || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrProviderExecutionUncertain) {
		return err
	}
	return nil
}

// CheckWorkflowProvider checks only provider availability. It cannot authorize a
// review launch: the prepared snapshot and admission are checked again there.
func (e *Engine) CheckWorkflowProvider(ctx context.Context, run domain.LocalWorkflowRun) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	worker, found, err := e.sessions.GetSession(ctx, run.SessionID)
	if err != nil {
		return err
	}
	if !found || worker.IsTerminated || strings.TrimSpace(run.WorkspacePath) == "" || worker.Metadata.WorkspacePath != run.WorkspacePath || domain.LocalWorkflowWorkerGeneration(worker) != run.WorkerGeneration {
		return fmt.Errorf("%w: workflow provider context changed", ErrConflict)
	}
	if err := run.ReviewerConfig.Validate(); err != nil {
		return fmt.Errorf("%w: invalid reviewer configuration", ErrInvalid)
	}
	check, ok := e.launcher.(workflowProviderLauncher)
	if !ok {
		return fmt.Errorf("%w: exact reviewer provider validation is unavailable", ErrInvalid)
	}
	return prelaunchProviderFailure(ctx, check.CheckWorkflowProvider(ctx, LaunchSpec{WorkerID: run.SessionID, ProjectID: worker.ProjectID, Harness: domain.ReviewerClaudeCode, TargetKind: domain.ReviewTargetLocal, WorkspacePath: run.WorkspacePath, ProjectEnv: maps.Clone(run.Env), AgentConfig: run.ReviewerConfig}))
}

// CheckWorkflowProvider uses the adapter's fresh provider round-trip, never a
// display/readiness cache or provider error prose. It starts no review process.
func (l *agentLauncher) CheckWorkflowProvider(ctx context.Context, spec LaunchSpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	reviewer, ok := l.reviewers.Reviewer(spec.Harness)
	if !ok {
		return fmt.Errorf("%w: reviewer adapter is unavailable", ErrInvalid)
	}
	validator, ok := reviewer.(ports.ReviewerLaunchAuthValidator)
	if !ok {
		return fmt.Errorf("%w: fresh reviewer provider validation is unsupported", ErrInvalid)
	}
	env := l.runtimeEnv(ctx, spec, nil, nil)
	status, diagnostics, err := validator.ValidateReviewLaunchAuth(ctx, spec.WorkspacePath, env)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return prelaunchProviderFailure(ctx, agentlaunch.RedactError(err, spec.ProjectEnv))
	}
	for i := len(diagnostics) - 1; i >= 0; i-- {
		d := diagnostics[i]
		if d.Method != "provider_validation" || d.Cached {
			continue
		}
		if status == ports.AgentAuthStatusAuthorized && d.Code == "provider_accepted" && d.HTTPStatus >= 200 && d.HTTPStatus < 300 {
			return nil
		}
		var reason domain.ProviderAccessReason
		switch {
		case d.HTTPStatus == 401 && d.Code == "provider_rejected":
			reason = domain.ProviderAccessAuthentication
		case d.HTTPStatus == 429:
			reason = domain.ProviderAccessRateLimited
		case d.HTTPStatus == 502 || d.HTTPStatus == 503 || d.HTTPStatus == 504:
			reason = domain.ProviderAccessUnavailable
		case d.HTTPStatus == 0 && (d.Code == "probe_timeout" || d.Code == "provider_unreachable"):
			reason = domain.ProviderAccessUnavailable
		}
		if reason.Valid() {
			var cause error
			if reason == domain.ProviderAccessAuthentication {
				cause = ports.ErrChatAuthRequired
			}
			return &domain.ProviderAccessError{Reason: reason, SafeToRetry: true, Cause: cause}
		}
	}
	return fmt.Errorf("%w: fresh reviewer provider access is unconfirmed", ErrInvalid)
}

// Only call before Spawn. A typed upstream failure does not itself prove that a
// process has ended; postlaunch paths explicitly retain an uncertainty fence.
func prelaunchProviderFailure(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, domain.ErrProviderExecutionUncertain) {
		return err
	}
	var access *domain.ProviderAccessError
	if errors.As(err, &access) && access != nil && access.Reason.Valid() {
		return &domain.ProviderAccessError{Reason: access.Reason, SafeToRetry: true, Cause: err}
	}
	if errors.Is(err, ports.ErrChatAuthRequired) {
		return &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication, SafeToRetry: true, Cause: err}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &domain.ProviderAccessError{Reason: domain.ProviderAccessUnavailable, SafeToRetry: true, Cause: err}
	}
	return err
}
