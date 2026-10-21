// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).
package domain

import (
	"fmt"
	"strings"
)

// LocalVerificationPlan records why and how this deliverable will be checked.
// A report has no fabricated command results; independent review is still required.
type LocalVerificationPlan struct {
	Profile   string   `json:"profile" enum:"implementation,report"`
	Rationale string   `json:"rationale"`
	Commands  []string `json:"commands"`
	Checks    []string `json:"checks"`
}

// Validate rejects missing verification criteria and untested implementation plans.
func (p LocalVerificationPlan) Validate() error {
	if (p.Profile != "implementation" && p.Profile != "report") || strings.TrimSpace(p.Rationale) == "" || len(p.Checks) == 0 {
		return fmt.Errorf("verification plan requires a profile, rationale and explicit checks")
	}
	if p.Profile == "implementation" && len(p.Commands) == 0 {
		return fmt.Errorf("implementation requires test commands; configure project checks or add a supported test script")
	}
	for _, text := range append(append([]string{}, p.Commands...), p.Checks...) {
		if strings.TrimSpace(text) == "" || strings.ContainsRune(text, 0) {
			return fmt.Errorf("verification plan contains an empty or invalid check")
		}
	}
	return nil
}
