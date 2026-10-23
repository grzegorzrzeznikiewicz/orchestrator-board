// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).
package localworkflow

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

// VerificationScopeRequest contains human scope selection, never executable commands.
type VerificationScopeRequest struct {
	WorkflowID string `json:"workflowId"`
	RequestID  string `json:"requestId"`
	ScopePath  string `json:"scopePath"`
	Basis      string `json:"basis"`
}

func scopedVerificationCandidates(root, scope string) ([]verificationCandidate, error) {
	if !utf8.ValidString(scope) || len(scope) > 256 || strings.ContainsAny(scope, "\\\x00\r\n") || filepath.IsAbs(scope) || scope == "." || scope == "" || scope != filepath.ToSlash(filepath.Clean(scope)) || scope == ".." || strings.HasPrefix(scope, "../") {
		return nil, fmt.Errorf("%w: verification scope must be a canonical relative package directory", domain.ErrLocalWorkflowInvalid)
	}
	if filepath.Dir(scope) != "." || !checkDiscoveryDirectory(scope) {
		return nil, fmt.Errorf("%w: verification scope must be a supported direct package directory", domain.ErrLocalWorkflowInvalid)
	}
	canonicalRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("%w: workspace is unavailable", domain.ErrLocalWorkflowInvalid)
	}
	path := filepath.Join(canonicalRoot, filepath.FromSlash(scope))
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, fmt.Errorf("%w: verification scope is unavailable or traverses a symlink", domain.ErrLocalWorkflowInvalid)
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return nil, fmt.Errorf("%w: verification scope must be an existing directory", domain.ErrLocalWorkflowInvalid)
	}
	// Never seed discovery with the proposed commands: explicit project commands
	// deliberately trust custom runners, while a package scope must be discovered.
	all, err := discoverChecksInDirectories(root, []string{scope}, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: discover verification checks: %w", domain.ErrLocalWorkflowInvalid, err)
	}
	var selected []verificationCandidate
	hasTest := false
	for _, candidate := range all {
		candidateScope, _, ok := checkInvocation(candidate.Command)
		if !ok || filepath.ToSlash(candidateScope) != scope || candidate.Kind != "test" && candidate.Kind != "static" {
			continue
		}
		if !slices.ContainsFunc(selected, func(c verificationCandidate) bool { return c.Command == candidate.Command }) {
			selected = append(selected, candidate)
		}
		hasTest = hasTest || candidate.Kind == "test"
	}
	if !hasTest || len(selected) > 32 {
		return nil, fmt.Errorf("%w: verification scope needs a bounded, discovered test command", domain.ErrLocalWorkflowInvalid)
	}
	return selected, nil
}

func validateVerificationContract(root string, contract *domain.LocalWorkflowVerificationContract) ([]verificationCandidate, error) {
	if contract.Version != 1 || !safeOperationID(contract.RequestID) || !utf8.ValidString(contract.RequestID) || strings.IndexFunc(contract.RequestID, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 || strings.TrimSpace(contract.Basis) == "" || len(contract.Basis) > 2048 || !utf8.ValidString(contract.Basis) || strings.ContainsRune(contract.Basis, 0) {
		return nil, fmt.Errorf("%w: verification contract version, request or human basis is invalid", domain.ErrLocalWorkflowInvalid)
	}
	candidates, err := scopedVerificationCandidates(root, contract.ScopePath)
	if err != nil {
		return nil, err
	}
	commands := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		commands = append(commands, candidate.Command)
	}
	if !slices.Equal(contract.Commands, commands) {
		return nil, fmt.Errorf("%w: discovered package checks changed; explicitly select the scope again", domain.ErrLocalWorkflowInvalid)
	}
	return candidates, nil
}

func validateScopedVerificationPlan(run domain.LocalWorkflowRun, plan domain.LocalVerificationPlan) error {
	if run.VerificationContract == nil {
		return nil
	}
	if _, err := validateVerificationContract(run.WorkspacePath, run.VerificationContract); err != nil {
		return err
	}
	if plan.Profile != "implementation" || len(plan.Commands) != len(run.VerificationContract.Commands) {
		return fmt.Errorf("scoped verification must execute all discovered package checks")
	}
	for _, command := range run.VerificationContract.Commands {
		if !slices.Contains(plan.Commands, command) {
			return fmt.Errorf("scoped verification omitted a required package check")
		}
	}
	return nil
}

func effectiveConfiguredCommands(run domain.LocalWorkflowRun, cfg domain.ProjectConfig) ([]string, error) {
	if run.VerificationContract == nil {
		return slices.Clone(cfg.TestCommands), nil
	}
	if _, err := validateVerificationContract(run.WorkspacePath, run.VerificationContract); err != nil {
		return nil, err
	}
	return slices.Clone(run.VerificationContract.Commands), nil
}

func requestedVerificationContract(run domain.LocalWorkflowRun, request *VerificationScopeRequest) (*domain.LocalWorkflowVerificationContract, error) {
	if request.WorkflowID != run.ID {
		return nil, fmt.Errorf("%w: verification scope belongs to another workflow", domain.ErrLocalWorkflowConflict)
	}
	if previous := run.VerificationContract; previous != nil && previous.RequestID == request.RequestID {
		if previous.ScopePath != request.ScopePath || previous.Basis != request.Basis {
			return nil, fmt.Errorf("%w: verification scope request already has different inputs", domain.ErrLocalWorkflowConflict)
		}
		if _, err := validateVerificationContract(run.WorkspacePath, previous); err != nil {
			return nil, err
		}
		cloned := *previous
		cloned.Commands = slices.Clone(previous.Commands)
		return &cloned, nil
	}
	candidates, err := scopedVerificationCandidates(run.WorkspacePath, request.ScopePath)
	if err != nil {
		return nil, err
	}
	contract := &domain.LocalWorkflowVerificationContract{Version: 1, ScopePath: request.ScopePath, Basis: request.Basis, RequestID: request.RequestID}
	for _, candidate := range candidates {
		contract.Commands = append(contract.Commands, candidate.Command)
	}
	if _, err = validateVerificationContract(run.WorkspacePath, contract); err != nil {
		return nil, err
	}
	return contract, nil
}
