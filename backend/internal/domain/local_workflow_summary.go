// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).
package domain

// LocalWorkflowCardFacts is a bounded display projection of the latest run.
// It contains no review bodies, prior runs, commands, environment, or authority
// to accept work. Run contains only the card's public checkpoint fields.
type LocalWorkflowCardFacts struct {
	SessionID           SessionID
	Run                 *LocalWorkflowRun
	LatestOperationKind string
	TestOperationID     string
	TestsPassed         int
	TestsTotal          int
	PendingIntent       bool
	ExecutorIdle        bool
	ReviewRunID         string
	ReviewValid         bool
	RequiredCount       int
	RequiredReason      string
}
