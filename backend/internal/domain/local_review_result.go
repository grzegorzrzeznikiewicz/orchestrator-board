package domain

import (
	"fmt"
	"path"
	"strings"
)

// LocalReviewResult is the versioned, snapshot-bound reviewer protocol.
// Outcome error is a failed review, never an approval or user acceptance.
type LocalReviewResult struct {
	Version    int                  `json:"version"`
	SnapshotID string               `json:"snapshotId"`
	Round      int                  `json:"round"`
	Outcome    string               `json:"outcome" enum:"approved,changes_requested,error"`
	Summary    string               `json:"summary"`
	Findings   []LocalReviewFinding `json:"findings"`
}

// LocalReviewFinding distinguishes actionable requirements from optional advice.
type LocalReviewFinding struct {
	Kind   string `json:"kind" enum:"required,suggestion"`
	Path   string `json:"path"`
	Line   int    `json:"line"`
	Reason string `json:"reason"`
}

// Validate refuses incomplete, mismatched and contradictory reviewer results.
func (r LocalReviewResult) Validate(snapshotID string, round int) error {
	if r.Version != 1 || r.SnapshotID != snapshotID || r.Round != round || round < 1 {
		return fmt.Errorf("local result must use version 1 and match snapshot and round")
	}
	if strings.TrimSpace(r.Summary) == "" || r.Findings == nil {
		return fmt.Errorf("local result requires summary and findings (use [] when empty)")
	}
	required := false
	for _, f := range r.Findings {
		if (f.Kind != "required" && f.Kind != "suggestion") || strings.TrimSpace(f.Reason) == "" || f.Line < 1 || strings.TrimSpace(f.Path) == "" || path.IsAbs(f.Path) || path.Clean(f.Path) != f.Path || f.Path == ".." || strings.HasPrefix(f.Path, "../") || strings.ContainsAny(f.Path, "\\:\x00\r\n") {
			return fmt.Errorf("each finding requires kind, repository-relative path, positive line and reason")
		}
		required = required || f.Kind == "required"
	}
	switch r.Outcome {
	case "approved":
		if required {
			return fmt.Errorf("approval cannot contain required changes")
		}
	case "changes_requested":
		if !required {
			return fmt.Errorf("changes_requested requires at least one required finding")
		}
	case "error":
	default:
		return fmt.Errorf("unknown local review outcome %q", r.Outcome)
	}
	return nil
}

// Markdown preserves a readable result for existing history consumers.
func (r LocalReviewResult) Markdown() string {
	var b strings.Builder
	b.WriteString(r.Summary)
	for _, f := range r.Findings {
		fmt.Fprintf(&b, "\n\n- **%s** — `%s:%d`: %s", f.Kind, f.Path, f.Line, f.Reason)
	}
	return b.String()
}
