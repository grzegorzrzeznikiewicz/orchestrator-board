// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
package domain

import "time"

// LocalWorkflowProviderRetry records bounded recovery facts for this workflow.
// The coordinator owns scheduling and reservations; clients only display these
// fields. Reason is an allowlisted observation, never raw provider output.
type LocalWorkflowProviderRetry struct {
	Stage       string               `json:"stage" enum:"planner,review"`
	State       string               `json:"state" enum:"waiting,checking,recovered,exhausted,uncertain"`
	Reason      ProviderAccessReason `json:"reason" enum:"authentication,rate_limited,unavailable"`
	Attempt     int                  `json:"attempt"`
	MaxAttempts int                  `json:"maxAttempts"`
	FailureAt   time.Time            `json:"failureAt"`
	LastAt      *time.Time           `json:"lastAt,omitempty"`
	NextAt      *time.Time           `json:"nextAt,omitempty"`
	OperationID string               `json:"operationId,omitempty"`
	ContextID   string               `json:"contextId,omitempty"`
	AttemptID   string               `json:"attemptId,omitempty"`
}
