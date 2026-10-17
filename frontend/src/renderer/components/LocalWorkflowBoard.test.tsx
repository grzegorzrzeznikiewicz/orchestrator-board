import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocalWorkflowBoard } from "./LocalWorkflowBoard";
import type { WorkspaceSession } from "../types/workspace";
const { get } = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("../lib/host-clients", () => ({
	clientForSessionHost: () => ({ GET: get }),
}));
afterEach(cleanup);
const session: WorkspaceSession = {
	id: "worker",
	title: "Task without PR",
	provider: "codex",
	workspaceId: "p",
	workspaceName: "Project",
	prs: [],
	status: "approved",
	kanbanColumn: "ready",
	updatedAt: "2026-10-17T12:00:00Z",
};
function show() {
	return render(
		<QueryClientProvider
			client={
				new QueryClient({ defaultOptions: { queries: { retry: false } } })
			}
		>
			<LocalWorkflowBoard
				sessions={[session]}
				renderSessionCard={(s, details) => (
					<article>
						{s.title}
						{details}
					</article>
				)}
			/>
		</QueryClientProvider>,
	);
}
describe("local workflow lanes", () => {
	it.each([
		["waiting_executor", "executing", "Execution"],
		["testing", "testing", "Tests"],
		["reviewing", "local_review", "Local review"],
		["fixing", "fixing", "Corrections"],
		["ready", "awaiting_acceptance", "Awaiting acceptance"],
		["blocked", "fixing", "Corrections"],
		["stopped", "testing", "Tests"],
	])(
		"uses daemon stage for %s even when the legacy status looks ready",
		async (phase, stage, lane) => {
			get.mockResolvedValue({
				data: {
					run: {
						id: "wf",
						phase,
						cycle: 2,
						maxRounds: 3,
						blockCode: phase === "blocked" ? "round_limit" : "",
						blockMessage: phase === "blocked" ? "Round limit reached" : "",
					},
					stage,
					current: phase === "ready",
					operations: [],
					tests: [],
					requiredFindings: [],
					reviews: [],
				},
			});
			show();
			const column = screen.getByRole("region", { name: `${lane} sessions` });
			expect(await within(column).findByText("Task without PR")).toBeVisible();
			expect(await within(column).findByText("Cycle 2 of 3")).toBeVisible();
			expect(
				within(
					screen.getByRole("region", { name: "Accepted sessions" }),
				).queryByText("Task without PR"),
			).toBeNull();
			if (phase === "blocked")
				expect(await screen.findByText("Round limit reached")).toBeVisible();
		},
	);
	it("shows the latest test results and required correction on the card", async () => {
		get.mockResolvedValue({
			data: {
				run: {
					id: "wf",
					phase: "blocked",
					cycle: 2,
					maxRounds: 3,
					testCommands: ["npm test"],
				},
				stage: "testing",
				current: false,
				operations: [
					{ id: "tests-2", kind: "tests", cycle: 2, state: "failed" },
				],
				tests: [{ id: "test", operationId: "tests-2", status: "failed" }],
				requiredFindings: [
					{
						kind: "required",
						path: "input.ts",
						line: 7,
						reason: "Reject empty input",
					},
				],
				reviews: [],
			},
		});
		show();
		expect(await screen.findByText("Tests: 0 of 1 passed")).toBeVisible();
		expect(screen.getByText("1 required finding")).toBeVisible();
		expect(screen.getByText("Reject empty input")).toBeVisible();
	});
	it("keeps stale approval out of awaiting acceptance and shows the reason", async () => {
		get.mockResolvedValue({
			data: {
				run: { id: "wf", phase: "ready", cycle: 2, maxRounds: 3 },
				stage: "local_review",
				current: false,
				operations: [],
				tests: [],
				requiredFindings: [],
				reviews: [],
			},
		});
		show();
		expect(
			await within(
				screen.getByRole("region", { name: "Local review sessions" }),
			).findByText("Task without PR"),
		).toBeVisible();
		expect(screen.getByText("Verification is out of date")).toBeVisible();
	});
	it("shows unavailable data without treating the worker's done status as success", async () => {
		get.mockRejectedValue(new Error("Daemon disconnected"));
		show();
		expect(await screen.findByRole("alert")).toHaveTextContent(
			"Daemon disconnected",
		);
		expect(
			within(
				screen.getByRole("region", { name: "Awaiting acceptance sessions" }),
			).queryByText("Task without PR"),
		).toBeNull();
	});
});
