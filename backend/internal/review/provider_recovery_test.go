// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package review

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
	"github.com/aoagents/agent-orchestrator/backend/internal/ports"
)

type workflowProviderProbe interface {
	CheckWorkflowProvider(context.Context, domain.LocalWorkflowRun) error
}

type providerRecoveryLauncher struct {
	*fakeLauncher
	checked  LaunchSpec
	checkErr error
}

func (l *providerRecoveryLauncher) CheckWorkflowProvider(_ context.Context, spec LaunchSpec) error {
	l.checked = spec
	return l.checkErr
}

func TestWorkflowProviderProbeUsesExactRunWithoutLaunching(t *testing.T) {
	e, _, launcher, worker := localEngine(t)
	l := &providerRecoveryLauncher{fakeLauncher: launcher}
	e.launcher = l
	probe, ok := any(e).(workflowProviderProbe)
	if !ok {
		t.Fatal("workflow recovery needs an exact-context, no-launch provider probe")
	}
	run := domain.LocalWorkflowRun{SessionID: worker.ID, WorkspacePath: worker.Metadata.WorkspacePath, WorkerGeneration: domain.LocalWorkflowWorkerGeneration(worker), Env: map[string]string{"PROJECT_CREDENTIAL": "private-value"}, ReviewerConfig: domain.AgentConfig{Model: "chosen-reviewer-model"}}
	if err := probe.CheckWorkflowProvider(t.Context(), run); err != nil {
		t.Fatal(err)
	}
	if launcher.spawned || l.checked.WorkspacePath != run.WorkspacePath || l.checked.AgentConfig != run.ReviewerConfig || !reflect.DeepEqual(l.checked.ProjectEnv, run.Env) || l.checked.Harness != domain.ReviewerClaudeCode {
		t.Fatalf("probe launched or changed the workflow context: %+v", l.checked)
	}
	run.WorkerGeneration = "replacement-controller"
	if err := probe.CheckWorkflowProvider(t.Context(), run); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed controller admitted: %v", err)
	}
}

func TestWorkflowPrelaunchProviderFailureKeepsDurableRunAndSafeReason(t *testing.T) {
	e, store, launcher, worker := localEngine(t)
	e.launcher = &providerRecoveryLauncher{fakeLauncher: launcher, checkErr: &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication, Cause: ports.ErrChatAuthRequired}}
	run := domain.LocalWorkflowRun{SessionID: worker.ID, WorkspacePath: worker.Metadata.WorkspacePath, WorkerGeneration: domain.LocalWorkflowWorkerGeneration(worker)}
	result, err := e.TriggerWithOptions(t.Context(), worker.ID, TriggerOptions{WorkflowRun: &run})
	reason, safe := domain.ProviderAccessFailure(err)
	if reason != domain.ProviderAccessAuthentication || !safe || !errors.Is(err, ports.ErrChatAuthRequired) || launcher.spawned || result.Run.ID == "" || result.Run.Status != domain.ReviewRunFailed {
		t.Fatalf("lost safe prelaunch cause or durable failed identity: result=%+v err=%v", result.Run, err)
	}
	stored, found, lookupErr := store.GetReviewRun(t.Context(), result.Run.ID)
	if lookupErr != nil || !found || stored.Status != domain.ReviewRunFailed {
		t.Fatalf("failed review was not durable: %+v %v", stored, lookupErr)
	}
}

type unsettledReviewResultStore struct {
	Store
	ports.LocalReviewWorkspace
	localReviewStore
	settleErr error
}

func (s unsettledReviewResultStore) UpdateReviewRunResult(context.Context, string, domain.ReviewRunStatus, domain.ReviewVerdict, string, string, bool) (bool, error) {
	return false, s.settleErr
}

func TestWorkflowPrelaunchFailureRequiresDurableSettlementBeforeRetry(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{{name: "storage error", err: errors.New("persistence unavailable")}, {name: "result unchanged"}} {
		t.Run(tc.name, func(t *testing.T) {
			e, store, launcher, worker := localEngine(t)
			e.store = unsettledReviewResultStore{Store: store, LocalReviewWorkspace: store, localReviewStore: store, settleErr: tc.err}
			e.launcher = &providerRecoveryLauncher{fakeLauncher: launcher, checkErr: &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication, Cause: ports.ErrChatAuthRequired}}
			run := domain.LocalWorkflowRun{SessionID: worker.ID, WorkspacePath: worker.Metadata.WorkspacePath, WorkerGeneration: domain.LocalWorkflowWorkerGeneration(worker)}
			result, err := e.TriggerWithOptions(t.Context(), worker.ID, TriggerOptions{WorkflowRun: &run})
			if _, safe := domain.ProviderAccessFailure(err); safe || !errors.Is(err, domain.ErrProviderExecutionUncertain) || result.Run.ID == "" || result.Run.Status != domain.ReviewRunRunning || launcher.spawned {
				t.Fatalf("unsettled failure authorized retry: run%+v err%v", result.Run, err)
			}
		})
	}
}

func TestWorkflowPostlaunchProviderFailureIsNeverSafe(t *testing.T) {
	e, _, launcher, worker := localEngine(t)
	launcher.spawnErr = &domain.ProviderAccessError{Reason: domain.ProviderAccessAuthentication, SafeToRetry: true, Cause: ports.ErrChatAuthRequired}
	run := domain.LocalWorkflowRun{SessionID: worker.ID, WorkspacePath: worker.Metadata.WorkspacePath, WorkerGeneration: domain.LocalWorkflowWorkerGeneration(worker)}
	result, err := e.TriggerWithOptions(t.Context(), worker.ID, TriggerOptions{WorkflowRun: &run})
	if _, safe := domain.ProviderAccessFailure(err); safe || !errors.Is(err, domain.ErrProviderExecutionUncertain) || !launcher.spawned || result.Run.ID == "" {
		t.Fatalf("uncertain launched reviewer can be replayed: run=%+v err=%v", result.Run, err)
	}
}

type postLaunchPersistenceStore struct {
	Store
	ports.LocalReviewWorkspace
	localReviewStore
	launched *bool
}

func (s postLaunchPersistenceStore) UpsertReview(ctx context.Context, r domain.Review) error {
	if *s.launched {
		return &domain.ProviderAccessError{Reason: domain.ProviderAccessUnavailable, SafeToRetry: true}
	}
	return s.Store.UpsertReview(ctx, r)
}

func TestWorkflowPostlaunchPersistenceFailureKeepsUncertaintyFence(t *testing.T) {
	e, store, launcher, worker := localEngine(t)
	e.store = postLaunchPersistenceStore{Store: store, LocalReviewWorkspace: store, localReviewStore: store, launched: &launcher.spawned}
	run := domain.LocalWorkflowRun{SessionID: worker.ID, WorkspacePath: worker.Metadata.WorkspacePath, WorkerGeneration: domain.LocalWorkflowWorkerGeneration(worker)}
	result, err := e.TriggerWithOptions(t.Context(), worker.ID, TriggerOptions{WorkflowRun: &run})
	if _, safe := domain.ProviderAccessFailure(err); safe || !errors.Is(err, domain.ErrProviderExecutionUncertain) || result.Run.ID == "" || !launcher.spawned {
		t.Fatalf("postlaunch persistence failure authorized replay: run%+v err%v", result.Run, err)
	}
}

func TestOrdinaryWorkflowReviewPreservesNativeRenewalAndUnsupportedProviderLaunch(t *testing.T) {
	for _, reason := range []string{"token_renewal_pending", "provider_unsupported"} {
		t.Run(reason, func(t *testing.T) {
			e, _, launcher, worker := localEngine(t)
			e.launcher = &providerRecoveryLauncher{fakeLauncher: launcher, checkErr: errors.New(reason)}
			run := domain.LocalWorkflowRun{SessionID: worker.ID, WorkspacePath: worker.Metadata.WorkspacePath, WorkerGeneration: domain.LocalWorkflowWorkerGeneration(worker)}
			result, err := e.TriggerWithOptions(t.Context(), worker.ID, TriggerOptions{WorkflowRun: &run})
			if err != nil || !launcher.spawned || result.Run.Status != domain.ReviewRunRunning {
				t.Fatalf("advisory auth uncertainty blocked native renewal/provider launch: %v", err)
			}
		})
	}
}

type diagnosticReviewer struct {
	fakeReviewer
	status   ports.AgentAuthStatus
	steps    []domain.AgentAuthDiagnostic
	probeErr error
	probes   int
	cwd      string
	env      map[string]string
}

type diagnosticPreflightReviewer struct {
	diagnosticReviewer
	preflightErr error
	compatCalled bool
}

func (r *diagnosticPreflightReviewer) ReviewCommand(context.Context, ports.ReviewInvocation) (ports.ReviewCommandSpec, error) {
	return ports.ReviewCommandSpec{Argv: []string{"go"}}, nil
}

func (r *diagnosticPreflightReviewer) ReviewPreflight(context.Context, string) error {
	r.compatCalled = true
	return r.preflightErr
}

func TestWorkflowPreflightUsesFreshExactProviderInsteadOfGlobalAuth(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     ports.AgentAuthStatus
		diagnostic domain.AgentAuthDiagnostic
		wantAuth   bool
		wantCompat bool
	}{
		{name: "exact success overrides unrelated global rejection", status: ports.AgentAuthStatusAuthorized, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_accepted", HTTPStatus: 200}, wantCompat: true},
		{name: "exact rejection blocks before compatibility", status: ports.AgentAuthStatusUnauthorized, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_rejected", HTTPStatus: 401}, wantAuth: true},
		{name: "unknown preserves existing preflight", status: ports.AgentAuthStatusConfigured, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "token_renewal_pending"}, wantAuth: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &diagnosticPreflightReviewer{diagnosticReviewer: diagnosticReviewer{status: tc.status, steps: []domain.AgentAuthDiagnostic{tc.diagnostic}}}
			rt := &fakeRuntime{}
			l := NewLauncher(fakeReviewerResolver{reviewer: r, ok: true}, rt, t.TempDir(), WithAgentAuth(fakeAgentAuthResolver{status: ports.AgentAuthStatusUnauthorized, ok: true}))
			preflight, ok := any(l).(interface {
				PreflightWorkflow(context.Context, LaunchSpec) error
			})
			if !ok {
				t.Fatal("workflow preflight must retain exact fresh provider evidence")
			}
			spec := launchSpec()
			spec.WorkspacePath = t.TempDir()
			spec.ProjectEnv = map[string]string{"ANTHROPIC_API_KEY": "exact-project-key"}
			err := preflight.PreflightWorkflow(t.Context(), spec)
			if errors.Is(err, ports.ErrChatAuthRequired) != tc.wantAuth || r.compatCalled != tc.wantCompat || rt.created || r.probes != 1 || r.env["ANTHROPIC_API_KEY"] != "exact-project-key" {
				t.Fatalf("preflight lost exact proof: err%v compatibility%v created%v", err, r.compatCalled, rt.created)
			}
			if !tc.wantAuth && err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestWorkflowTriggerRetainsFreshProviderProofThroughNativePreflight(t *testing.T) {
	e, _, _, worker := localEngine(t)
	r := &diagnosticPreflightReviewer{diagnosticReviewer: diagnosticReviewer{status: ports.AgentAuthStatusAuthorized, steps: []domain.AgentAuthDiagnostic{{Method: "provider_validation", Code: "provider_accepted", HTTPStatus: 200}}}}
	rt := &fakeRuntime{}
	e.launcher = NewLauncher(fakeReviewerResolver{reviewer: r, ok: true}, rt, t.TempDir(), WithAgentAuth(fakeAgentAuthResolver{status: ports.AgentAuthStatusUnauthorized, ok: true}))
	run := domain.LocalWorkflowRun{SessionID: worker.ID, WorkspacePath: worker.Metadata.WorkspacePath, WorkerGeneration: domain.LocalWorkflowWorkerGeneration(worker), Env: map[string]string{"ANTHROPIC_API_KEY": "exact-project-key"}}
	result, err := e.TriggerWithOptions(t.Context(), worker.ID, TriggerOptions{WorkflowRun: &run})
	if err != nil || !rt.created || result.Run.Status != domain.ReviewRunRunning || r.probes != 1 || !r.compatCalled || r.cwd != result.Run.SnapshotPath || r.env["ANTHROPIC_API_KEY"] != "exact-project-key" {
		t.Fatalf("native launch lost its tested credential: run%+v probes%d created%v err%v", result.Run, r.probes, rt.created, err)
	}
}

func (r *diagnosticReviewer) ValidateReviewLaunchAuth(_ context.Context, cwd string, env map[string]string) (ports.AgentAuthStatus, []domain.AgentAuthDiagnostic, error) {
	r.probes++
	r.cwd, r.env = cwd, env
	return r.status, r.steps, r.probeErr
}

func TestWorkflowLauncherClassifiesOnlyFreshStructuredProviderEvidence(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     ports.AgentAuthStatus
		diagnostic domain.AgentAuthDiagnostic
		reason     domain.ProviderAccessReason
		safe       bool
		err        error
	}{
		{name: "accepted", status: ports.AgentAuthStatusAuthorized, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_accepted", HTTPStatus: 200}},
		{name: "unauthorized", status: ports.AgentAuthStatusUnauthorized, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_rejected", HTTPStatus: 401}, reason: domain.ProviderAccessAuthentication, safe: true},
		{name: "rate limited", diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_inconclusive", HTTPStatus: 429}, reason: domain.ProviderAccessRateLimited, safe: true},
		{name: "unavailable", diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_inconclusive", HTTPStatus: 503}, reason: domain.ProviderAccessUnavailable, safe: true},
		{name: "bad gateway", diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_inconclusive", HTTPStatus: 502}, reason: domain.ProviderAccessUnavailable, safe: true},
		{name: "gateway timeout", diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_inconclusive", HTTPStatus: 504}, reason: domain.ProviderAccessUnavailable, safe: true},
		{name: "unreachable", diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_unreachable"}, reason: domain.ProviderAccessUnavailable, safe: true},
		{name: "timeout", diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "probe_timeout"}, reason: domain.ProviderAccessUnavailable, safe: true},
		{name: "forbidden", diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_inconclusive", HTTPStatus: 403}},
		{name: "cached", status: ports.AgentAuthStatusAuthorized, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_accepted", HTTPStatus: 200, Cached: true}},
		{name: "cached rejection", status: ports.AgentAuthStatusUnauthorized, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_rejected", HTTPStatus: 401, Cached: true}},
		{name: "local signed out", status: ports.AgentAuthStatusUnauthorized, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "credential_missing"}},
		{name: "signed out with rate limit", status: ports.AgentAuthStatusUnauthorized, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "provider_inconclusive", HTTPStatus: 429}, reason: domain.ProviderAccessRateLimited, safe: true},
		{name: "configured only", status: ports.AgentAuthStatusConfigured, diagnostic: domain.AgentAuthDiagnostic{Method: "provider_validation", Code: "credential_present"}},
		{name: "prose", err: errors.New("HTTP401 token expired; refresh and retry")},
		{name: "cancelled", err: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &diagnosticReviewer{status: tc.status, steps: []domain.AgentAuthDiagnostic{tc.diagnostic}, probeErr: tc.err}
			rt := &fakeRuntime{}
			launcher := NewLauncher(fakeReviewerResolver{reviewer: r, ok: true}, rt, t.TempDir())
			check, ok := any(launcher).(interface {
				CheckWorkflowProvider(context.Context, LaunchSpec) error
			})
			if !ok {
				t.Fatal("launcher lacks fresh exact-context provider validation")
			}
			spec := launchSpec()
			spec.WorkspacePath = t.TempDir()
			spec.ProjectEnv = map[string]string{"PROJECT_CREDENTIAL": "exact-project-value"}
			err := check.CheckWorkflowProvider(t.Context(), spec)
			reason, safe := domain.ProviderAccessFailure(err)
			if safe != tc.safe || reason != tc.reason || rt.created {
				t.Fatalf("classification reason=%q safe=%v created=%v err=%v", reason, safe, rt.created, err)
			}
			if tc.name == "accepted" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("inconclusive or failed proof granted access")
			}
			if r.cwd != spec.WorkspacePath || r.env["PROJECT_CREDENTIAL"] != "exact-project-value" {
				t.Fatal("fresh validation used the wrong project context")
			}
		})
	}
}
