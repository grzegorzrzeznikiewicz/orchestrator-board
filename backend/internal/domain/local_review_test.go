package domain

import "testing"

func TestLocalReviewProjectConfig(t *testing.T) {
	for _, tc := range []struct {
		name    string
		config  ProjectConfig
		invalid bool
	}{
		{"legacy", ProjectConfig{}, false},
		{"pr", ProjectConfig{ReviewMode: ReviewTargetPR}, false},
		{"local defaults", ProjectConfig{ReviewMode: ReviewTargetLocal}, false},
		{"test commands", ProjectConfig{ReviewMode: ReviewTargetLocal, TestCommands: []string{"go test ./...", "npm test"}}, false},
		{"unknown mode", ProjectConfig{ReviewMode: "other"}, true},
		{"blank command", ProjectConfig{TestCommands: []string{"  "}}, true},
		{"nul command", ProjectConfig{TestCommands: []string{"npm\x00test"}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.config.Validate(); (err != nil) != tc.invalid {
				t.Fatalf("Validate() = %v", err)
			}
		})
	}
	c := (ProjectConfig{ReviewMode: ReviewTargetLocal}).WithDefaults()
	if c.Worker.Harness != HarnessCodex || len(c.Reviewers) != 1 || c.Reviewers[0].Harness != ReviewerClaudeCode {
		t.Fatalf("local defaults = %+v", c)
	}
	if (ProjectConfig{}).WithDefaults().EffectiveReviewMode() != ReviewTargetPR {
		t.Fatal("legacy project must use PR review")
	}
}
