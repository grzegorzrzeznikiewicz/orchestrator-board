// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { expect, test } from "@playwright/test";
import type { LocalWorkflowCard, LocalWorkflowSnapshot } from "../src/renderer/lib/local-workflow-api";
import type { WorkspaceSummary } from "../src/renderer/types/workspace";
import { agentReadiness } from "../src/renderer/test/agent-readiness-fixtures";
import { installFakeAgent } from "./support/fake-bridge";

// Controlled renderer coverage: fake preload/workspaces/CDC and HTTP responses.
// No daemon, native decision dialog, executor, or reviewer runs in this test.
// Backend tests separately prove the transitions represented by these fixtures.
test("renderer: automatic local verification updates lanes and invalidates old acceptance @P0", async ({ page }) => {
	test.setTimeout(60_000);
	const projectId = "automatic-workflow";
	const projectName = "Automatic workflow project";
	const sessionId = "automatic-worker";
	const now = "2026-10-19T10:00:00Z";
	const title = "Automatically verified task";
	const implementationPlan = {
		profile: "implementation" as const,
		rationale: "The task changes parsing behavior",
		commands: ["npm run test:parser", "npm run lint"],
		checks: ["Malformed input is rejected"],
	};
	const run: NonNullable<LocalWorkflowSnapshot["run"]> = {
		id: "workflow-implementation", sessionId, version: 1,
		phase: "testing", cycle: 1, maxRounds: 3, stopRequested: false,
		automatic: true, planState: "complete", verificationPlan: implementationPlan,
		testCommands: implementationPlan.commands, createdAt: now, updatedAt: now,
	};
	const testsOperation: LocalWorkflowSnapshot["operations"][number] = {
		id: "tests-operation", workflowId: run.id, kind: "tests", cycle: 1,
		attempt: 1, state: "running", snapshotId: "snapshot", startedAt: now,
	};
	let snapshot: LocalWorkflowSnapshot = {
		run: null, automation: { pending: true }, stage: "executing",
		current: false, accepted: false, decisions: [], operations: [],
		tests: [], reviews: [], requiredFindings: [],
	};
	const workflowMutations: string[] = [];
	const summaryRequests: string[][] = [];
	let workflowDetailReads = 0;
	const cardSummary = (): LocalWorkflowCard => {
		const currentRun = snapshot.run;
		const testsOperation = [...snapshot.operations].reverse().find((operation) => operation.kind === "tests" && operation.cycle === currentRun?.cycle);
		const tests = snapshot.tests.filter((result) => result.operationId === testsOperation?.id);
		return {
			sessionId, stage: snapshot.stage, current: snapshot.current, accepted: snapshot.accepted,
			automation: snapshot.automation ?? { pending: false },
			run: currentRun ? {
				id: currentRun.id, version: currentRun.version, phase: currentRun.phase,
				cycle: currentRun.cycle, maxRounds: currentRun.maxRounds, automatic: currentRun.automatic,
				planState: currentRun.planState, profile: currentRun.verificationPlan?.profile,
				providerRetry: currentRun.providerRetry,
				blockCode: currentRun.blockCode, blockMessage: currentRun.blockMessage,
			} : null,
			testsPresent: !!testsOperation, testsPassed: tests.filter((result) => result.status === "succeeded").length,
			testsTotal: currentRun?.testCommands.length ?? 0, testsQueued: false,
			requiredCount: snapshot.requiredFindings.length, requiredReason: snapshot.requiredFindings[0]?.reason,
		};
	};
	await page.setViewportSize({ width: 1920, height: 1080 });
	await page.emulateMedia({ reducedMotion: "reduce" });
	await installFakeAgent(page, {
		projectId, projectName,
		workers: [{ id: sessionId, title, mode: "chat", provider: "codex", activity: "idle" }],
	});
	await page.addInitScript(({ sessionId, projectId }) => {
		const fake = window.__aoFakeAgent!;
		const originalSnapshot = fake.snapshot;
		fake.snapshot = () => (originalSnapshot() as WorkspaceSummary[]).map((workspace) => ({
			...workspace, reviewMode: "local",
		}));
		// This spec exercises session-scoped workflow invalidation. The generic
		// fake helper emits an empty workspace notification; give those updates the
		// durable CDC envelope that the real daemon sends for this session.
		const sourcePrototype = window.EventSource.prototype as unknown as {
			_dispatch: (type: string, data?: string) => void;
		};
		const dispatch = sourcePrototype._dispatch;
		let sequence = 0;
		sourcePrototype._dispatch = function (type, data) {
			dispatch.call(this, type, type === "session_updated" && !data
				? JSON.stringify({ seq: ++sequence, type, projectId, sessionId, payload: { sessionId } })
				: data);
		};
		// Exercise enabled renderer decision controls without invoking native code.
		window.ao!.localWorkflow.available = true;
	}, { sessionId, projectId });
	await page.route("http://127.0.0.1:8080/api/v1/**", async (route) => {
		const url = new URL(route.request().url());
		const path = url.pathname;
		if (path.includes("/local-workflow") && route.request().method() !== "GET") {
			workflowMutations.push(path);
			await route.fulfill({ status: 409, json: { error: { message: "Unexpected renderer workflow mutation" } } });
		} else if (path === "/api/v1/local-workflow/summaries") {
			const ids = (url.searchParams.get("sessionIds") ?? "").split(",");
			summaryRequests.push(ids);
			await route.fulfill({ json: { summaries: ids.includes(sessionId) ? [cardSummary()] : [] } });
		} else if (path === `/api/v1/sessions/${sessionId}/local-workflow`) {
			workflowDetailReads++;
			await route.fulfill({ json: snapshot });
		} else if (path === `/api/v1/projects/${projectId}`) {
			await route.fulfill({ json: { project: { id: projectId, config: {
				reviewMode: "local", worker: { agent: "codex" }, reviewers: [{ harness: "claude-code" }],
			} } } });
		} else if (path.endsWith("/agents/readiness") || path.endsWith("/agents/readiness/ensure")) {
			await route.fulfill({ json: { agents: [agentReadiness("codex", "Codex"), agentReadiness("claude-code", "Claude Code")] } });
		} else if (path.endsWith("/agents/installers")) {
			await route.fulfill({ json: { agents: [] } });
		} else if (path.endsWith("/agents/auth-plans")) {
			await route.fulfill({ json: { plans: [] } });
		} else if (path.endsWith("/agents/install-jobs")) {
			await route.fulfill({ json: { jobs: [] } });
		} else if (path === `/api/v1/sessions/${sessionId}/reviews`) {
			await route.fulfill({ json: { reviewerHandleId: "", reviews: [], runs: [], activeReviewers: [] } });
		} else if (path === `/api/v1/sessions/${sessionId}/conversation`) {
			await route.fulfill({ json: {
				conversationId: "automatic-conversation", sessionId, harness: "codex", mode: "chat", controller: "ready",
				latestSequence: 0, oldestSequence: 0, hasMoreBefore: false, turns: [], messages: [], activities: [], settings: {},
			} });
		} else if (path.endsWith("/conversation/models")) {
			await route.fulfill({ json: { models: [], selected: {} } });
		} else if (path.endsWith("/conversation/skills")) {
			await route.fulfill({ json: { skills: [] } });
		} else if (path.endsWith("/workspace/files")) {
			await route.fulfill({ json: { files: [], truncated: false } });
		} else {
			await route.fulfill({ json: { status: "ok" } });
		}
	});
	const pushUpdate = async () => {
		await page.evaluate((id) => window.__aoFakeAgent!.setStatus(id, "working", "idle"), sessionId);
	};
	const board = page.getByTestId("board");
	const lane = (stage: string) => board.locator(`section[data-column="${stage}"]`);
	const card = (stage: string) => lane(stage).locator(`[data-session-id="${sessionId}"]`);
	const panel = page.locator('#inspector section[aria-label="Local workflow"]');
	const openBoard = async () => {
		await page.getByRole("button", { name: projectName, exact: true }).click();
		await expect(board).toBeVisible();
	};

	await page.goto(`/#/projects/${projectId}`);
	await expect(board.locator('section[aria-label="Local workflow"] [data-column]')).toHaveCount(6);
	await expect(card("executing")).toContainText("Preparing automatic verification…");
	expect(summaryRequests.length).toBeGreaterThan(0);
	expect(summaryRequests.every((ids) => ids.length === 1 && ids[0] === sessionId)).toBe(true);
	expect(workflowDetailReads).toBe(0);
	await card("executing").click();
	await expect(panel.getByText("Preparing automatic verification…")).toBeVisible();
	await expect(panel.getByRole("button", { name: "Start workflow" })).toHaveCount(0);

	// These are controlled display facts, not actual provider recovery executions.
	const nextAt = "2026-10-20T10:02:00Z";
	const retry = { stage: "planner" as const, state: "waiting" as const, reason: "authentication" as const, attempt: 0, maxAttempts: 3, failureAt: now, nextAt };
	snapshot = { ...snapshot, run: { ...run, phase: "blocked", blockCode: "provider_waiting", providerRetry: retry }, automation: { pending: false }, stage: "executing" };
	await pushUpdate();
	await expect(panel.getByText("Waiting for provider access", { exact: true })).toBeVisible();
	await expect(panel.getByText("Provider authentication needs recovery", { exact: true })).toBeVisible();
	await expect(panel.getByText(/^Next attempt:/)).toHaveAttribute("datetime", nextAt);
	await expect(panel.getByRole("button", { name: "Stop workflow", exact: true })).toBeEnabled();
	await expect(panel.getByRole("button", { name: "Accept changes", exact: true })).toHaveCount(0);
	await openBoard();
	await expect(card("executing")).toContainText("Waiting for provider access");
	await expect(card("executing").getByText(/^Next attempt:/)).toHaveAttribute("datetime", nextAt);

	snapshot = { ...snapshot, run: { ...snapshot.run!, version: 2, providerRetry: { ...retry, state: "checking", attempt: 1, lastAt: now, nextAt: undefined } } };
	await pushUpdate();
	await expect(card("executing")).toContainText("Checking provider access");
	await expect(card("executing").getByText(/^Next attempt:/)).toHaveCount(0);
	await card("executing").click();
	await expect(panel.getByText("Checking provider access", { exact: true })).toBeVisible();
	await expect(panel.getByRole("button", { name: "Stop workflow", exact: true })).toBeEnabled();

	snapshot = { ...snapshot, run: { ...snapshot.run!, version: 3, blockCode: "provider_uncertain", blockMessage: "Previous provider termination is unconfirmed", providerRetry: { ...retry, state: "uncertain", attempt: 1, nextAt: undefined } } };
	await pushUpdate();
	await expect(panel.getByText("Provider retry completion is unconfirmed", { exact: true })).toBeVisible();
	await expect(panel.getByRole("button", { name: "Resume workflow", exact: true })).toBeDisabled();
	await expect(panel.getByText("End the affected session and confirm that its provider process has ended before starting another task.", { exact: true })).toBeVisible();
	await expect(panel.getByText(/^Next attempt:/)).toHaveCount(0);
	await openBoard();
	await expect(card("executing")).toContainText("Provider retry completion is unconfirmed");

	// A separate recovered display case follows; these fixtures do not model a
	// permitted backend transition out of uncertainty or send Resume themselves.
	snapshot = { ...snapshot, run: { ...run, version: 4, phase: "waiting_executor", providerRetry: { ...retry, state: "recovered", attempt: 1, nextAt: undefined } } };
	await pushUpdate();
	await expect(card("executing")).toContainText("Provider access recovered");
	await expect(card("executing")).not.toContainText("Waiting for provider access");
	await card("executing").click();
	await expect(panel.getByText("Provider access recovered", { exact: true })).toBeVisible();
	await expect(panel.getByText(/^Next attempt:/)).toHaveCount(0);
	expect(workflowMutations).toEqual([]);

	snapshot = { ...snapshot, run: { ...run, version: 5 }, stage: "testing", operations: [testsOperation] };
	await pushUpdate();
	const plan = panel.getByRole("region", { name: "Verification plan" });
	await expect(plan.getByText("Code changes", { exact: true })).toBeVisible();
	await expect(plan.getByText("npm run test:parser", { exact: true })).toBeVisible();
	await expect(plan.getByText("npm run lint", { exact: true })).toBeVisible();
	await expect(plan.getByText("Malformed input is rejected", { exact: true })).toBeVisible();
	await openBoard();
	await expect(card("testing")).toContainText("Running tests");
	await expect(card("executing")).toHaveCount(0);

	snapshot = { ...snapshot, run: { ...run, version: 6, phase: "reviewing" }, stage: "local_review" };
	await pushUpdate();
	await expect(card("local_review")).toContainText("Claude review");
	await expect(card("testing")).toHaveCount(0);
	await card("local_review").click();
	await expect(panel.getByRole("status")).toContainText("Claude review");

	const reportRun = { ...run, id: "workflow-report", version: 7, phase: "ready" as const, testCommands: [], verificationPlan: {
		profile: "report" as const, rationale: "The follow-up requests an assessment only", commands: [], checks: ["Conclusions cite observed evidence"],
	} };
	snapshot = {
		...snapshot, run: reportRun, stage: "awaiting_acceptance", current: true,
		previousRuns: [{ ...run, phase: "stopped", closedAt: now }],
		operations: [
			{ ...testsOperation, workflowId: reportRun.id, state: "succeeded", completedAt: now },
			{ id: "review-operation", workflowId: reportRun.id, kind: "review", cycle: 1, attempt: 1, state: "succeeded", snapshotId: "snapshot", inputOperationId: testsOperation.id, reviewRunId: "report-review", startedAt: now, completedAt: now },
		],
		reviews: [{
			id: "report-review", reviewId: "reviewer", sessionId, batchId: "batch", harness: "claude-code", status: "complete",
			createdAt: now, targetKind: "local", snapshotId: "snapshot", current: true, round: 1, triggerSource: "auto",
			autoInjectReview: false, body: "", githubReviewId: "", prUrl: "", targetSha: "", verdict: "approved",
			localResult: { version: 1, snapshotId: "snapshot", round: 1, outcome: "approved", summary: "The report is supported by the observed evidence", findings: [] },
		}],
	};
	await pushUpdate();
	await expect(panel.getByRole("button", { name: "Accept changes", exact: true })).toBeEnabled();
	await expect(plan.getByText("Report", { exact: true })).toBeVisible();
	await expect(plan.getByText("Automated tests: not applicable to this report.", { exact: true })).toBeVisible();
	await expect(panel.getByText("The reviewer approved this report. Ready for your acceptance.")).toBeVisible();
	await expect(panel.getByText(/Tests passed|0 of 0 passed/)).toHaveCount(0);
	await openBoard();
	await expect(card("awaiting_acceptance")).toContainText("Automated tests: not applicable to this report.");
	await expect(card("awaiting_acceptance")).not.toContainText("0 of 0 passed");
	await card("awaiting_acceptance").click();
	await expect(panel.getByRole("button", { name: "Accept changes", exact: true })).toBeEnabled();

	snapshot = { ...snapshot, stage: "accepted", accepted: true };
	await pushUpdate();
	await openBoard();
	await expect(card("accepted")).toContainText("Accepted");
	await expect(card("accepted")).not.toContainText("Ready for your acceptance");
	await card("accepted").click();

	snapshot = { ...snapshot, automation: { pending: true, blockMessage: "Waiting for the previous test process to stop" }, stage: "executing", current: false, accepted: false };
	await pushUpdate();
	await expect(panel.getByRole("alert")).toHaveText("Waiting for the previous test process to stop");
	await expect(panel.getByRole("button", { name: "Accept changes", exact: true })).toHaveCount(0);
	await expect(panel.getByRole("button", { name: "Start workflow", exact: true })).toHaveCount(0);
	await openBoard();
	await expect(card("executing")).toContainText("Preparing automatic verification…");
	await expect(card("executing").getByRole("alert")).toHaveText("Waiting for the previous test process to stop");
	await expect(card("awaiting_acceptance")).toHaveCount(0);
	await expect(card("accepted")).toHaveCount(0);
	expect(workflowMutations).toEqual([]);
});
