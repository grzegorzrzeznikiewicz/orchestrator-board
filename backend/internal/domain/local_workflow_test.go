// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package domain

import "testing"

func TestLocalWorkflowConfiguration(t *testing.T) {
	for _, tc := range []struct {
		name    string
		value   int
		want    int
		invalid bool
	}{
		{"default", 0, 3, false}, {"explicit", 5, 5, false}, {"negative", -1, -1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := ProjectConfig{ReviewMode: ReviewTargetLocal, LocalWorkflowMaxRounds: tc.value}
			if (cfg.Validate() != nil) != tc.invalid {
				t.Fatal("unexpected validation result")
			}
			if got := cfg.EffectiveLocalWorkflowMaxRounds(); got != tc.want {
				t.Fatalf("limit = %d, want %d", got, tc.want)
			}
		})
	}
	if err := (ProjectConfig{ReviewMode: ReviewTargetLocal}).Validate(); err != nil {
		t.Fatalf("manual review still accepts no commands: %v", err)
	}
	if (ProjectConfig{}).EffectiveReviewMode() != ReviewTargetPR {
		t.Fatal("legacy mode changed")
	}
}
