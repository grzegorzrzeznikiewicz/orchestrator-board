package domain

// ReviewTargetKind distinguishes a provider PR commit from local file contents.
type ReviewTargetKind string

// Supported review targets distinguish provider commits from local snapshots.
const (
	ReviewTargetPR    ReviewTargetKind = "pr"
	ReviewTargetLocal ReviewTargetKind = "local"
)

// EffectiveReviewMode preserves PR review for configurations predating local review.
func (c ProjectConfig) EffectiveReviewMode() ReviewTargetKind {
	if c.ReviewMode == "" {
		return ReviewTargetPR
	}
	return c.ReviewMode
}
