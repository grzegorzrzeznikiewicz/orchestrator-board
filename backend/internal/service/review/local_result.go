package review

import (
	"context"
	"fmt"
	"reflect"

	"github.com/aoagents/agent-orchestrator/backend/internal/domain"
)

type localResultStore interface {
	UpdateLocalReviewResult(context.Context, string, domain.ReviewRunStatus, domain.ReviewVerdict, string, *domain.LocalReviewResult) (bool, error)
}

func (s *Service) submitLocal(ctx context.Context, run domain.ReviewRun, review SubmittedReview) (domain.ReviewRun, error) {
	store, ok := s.store.(localResultStore)
	if !ok || s.engine == nil {
		return domain.ReviewRun{}, fmt.Errorf("%w: local result persistence or snapshot verification unavailable", ErrConflict)
	}
	if run.Status != domain.ReviewRunRunning {
		if review.LocalResult != nil && reflect.DeepEqual(run.LocalResult, review.LocalResult) && review.Verdict == "" && review.Body == "" && review.GithubReviewID == "" && (run.Status == domain.ReviewRunComplete || run.Status == domain.ReviewRunFailed) {
			if err := s.engine.ValidateLocalRun(ctx, run); err != nil {
				return domain.ReviewRun{}, err
			}
			run.Current = true
			return run, nil
		}
		return domain.ReviewRun{}, fmt.Errorf("%w: local review is no longer running", ErrConflict)
	}
	if err := s.engine.ValidateLocalRun(ctx, run); err != nil {
		return domain.ReviewRun{}, err
	}
	var invalid error
	if review.LocalResult == nil {
		invalid = fmt.Errorf("versioned localResult is required")
	} else {
		invalid = review.LocalResult.Validate(run.SnapshotID, run.Round)
	}
	if review.Verdict != "" || review.Body != "" || review.GithubReviewID != "" {
		invalid = fmt.Errorf("localResult must not include a separate verdict, body or provider review id")
	}
	if invalid != nil {
		if _, err := store.UpdateLocalReviewResult(ctx, run.ID, domain.ReviewRunFailed, domain.VerdictNone, "Invalid local review result: "+invalid.Error(), nil); err != nil {
			return domain.ReviewRun{}, err
		}
		return domain.ReviewRun{}, fmt.Errorf("%w: %w", ErrInvalid, invalid)
	}
	result := review.LocalResult
	status := domain.ReviewRunComplete
	verdict := domain.ReviewVerdict(result.Outcome)
	if result.Outcome == "error" {
		status = domain.ReviewRunFailed
		verdict = domain.VerdictNone
	}
	body := result.Markdown()
	updated, err := store.UpdateLocalReviewResult(ctx, run.ID, status, verdict, body, result)
	if err != nil {
		return domain.ReviewRun{}, err
	}
	if !updated {
		return domain.ReviewRun{}, fmt.Errorf("%w: local review ended or was invalidated before result storage", ErrConflict)
	}
	run.Status = status
	run.Verdict = verdict
	run.Body = body
	run.LocalResult = result
	if err := s.engine.ValidateLocalRun(ctx, run); err != nil {
		return domain.ReviewRun{}, err
	}
	run.Current = true
	if status == domain.ReviewRunComplete {
		s.emitReviewNotification(ctx, run)
	}
	return run, nil
}
