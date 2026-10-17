export type LocalWorkflowDecisionInput = {
	sessionId: string;
	requestId: string;
	workflowId: string;
	expectedVersion: number;
	kind: "accepted" | "changes_requested";
	comment: string;
	snapshotId: string;
	testOperationId: string;
	reviewRunId: string;
};

export type LocalWorkflowDecisionReply = { status: "recorded" | "cancelled" };
export const LOCAL_WORKFLOW_DECIDE_CHANNEL = "localWorkflow:decide";
export const LOCAL_WORKFLOW_CONFIRM_ACTION =
	"__confirm-local-workflow-decision";
