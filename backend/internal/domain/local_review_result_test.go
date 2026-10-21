// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

package domain

import "testing"

func TestLocalReviewResultValidation(t *testing.T) {
	valid := LocalReviewResult{Version: 1, SnapshotID: "snapshot", Round: 2, Outcome: "approved", Summary: "All acceptance criteria met", Findings: []LocalReviewFinding{}}
	for _, tc := range []struct {
		name    string
		change  func(*LocalReviewResult)
		wantErr bool
	}{
		{"approved", func(*LocalReviewResult) {}, false},
		{"suggestion", func(r *LocalReviewResult) {
			r.Findings = []LocalReviewFinding{{Kind: "suggestion", Path: "new.go", Line: 1, Reason: "Consider a clearer name"}}
		}, false},
		{"required", func(r *LocalReviewResult) {
			r.Outcome = "changes_requested"
			r.Findings = []LocalReviewFinding{{Kind: "required", Path: "new.go", Line: 2, Reason: "Nil input panics"}}
		}, false},
		{"error", func(r *LocalReviewResult) { r.Outcome = "error" }, false},
		{"missing version", func(r *LocalReviewResult) { r.Version = 0 }, true},
		{"wrong snapshot", func(r *LocalReviewResult) { r.SnapshotID = "other" }, true},
		{"wrong round", func(r *LocalReviewResult) { r.Round = 1 }, true},
		{"unknown outcome", func(r *LocalReviewResult) { r.Outcome = "ok" }, true},
		{"empty summary", func(r *LocalReviewResult) { r.Summary = " " }, true},
		{"missing findings", func(r *LocalReviewResult) { r.Findings = nil }, true},
		{"no required changes", func(r *LocalReviewResult) { r.Outcome = "changes_requested" }, true},
		{"approval with blocker", func(r *LocalReviewResult) {
			r.Findings = []LocalReviewFinding{{Kind: "required", Path: "new.go", Line: 1, Reason: "Bug"}}
		}, true},
		{"missing reason", func(r *LocalReviewResult) {
			r.Findings = []LocalReviewFinding{{Kind: "suggestion", Path: "new.go", Line: 1}}
		}, true},
		{"missing line", func(r *LocalReviewResult) {
			r.Findings = []LocalReviewFinding{{Kind: "suggestion", Path: "new.go", Reason: "Reason"}}
		}, true},
		{"path traversal", func(r *LocalReviewResult) {
			r.Findings = []LocalReviewFinding{{Kind: "suggestion", Path: "../secret", Line: 1, Reason: "Reason"}}
		}, true},
		{"absolute path", func(r *LocalReviewResult) {
			r.Findings = []LocalReviewFinding{{Kind: "suggestion", Path: "/secret", Line: 1, Reason: "Reason"}}
		}, true},
		{"unknown kind", func(r *LocalReviewResult) {
			r.Findings = []LocalReviewFinding{{Kind: "note", Path: "new.go", Line: 1, Reason: "Reason"}}
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := valid
			tc.change(&r)
			if err := r.Validate("snapshot", 2); (err != nil) != tc.wantErr {
				t.Fatalf("validation=%v wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
