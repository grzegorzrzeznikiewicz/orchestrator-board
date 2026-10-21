// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { LocalWorkflowProviderRetry } from "./LocalWorkflowProviderRetry";
import type { LocalWorkflowProviderRetryFacts } from "../lib/local-workflow-api";

const retry = {
	stage: "review" as const, state: "waiting" as const, reason: "authentication" as const,
	attempt: 1, maxAttempts: 3, failureAt: "2026-10-20T10:00:00Z",
	lastAt: "2026-10-20T10:01:00Z", nextAt: "2026-10-20T10:02:00Z",
};
afterEach(cleanup);
describe("provider retry facts", () => {
	it("shows the daemon's safe waiting reason, budget and exact next attempt without actions", () => {
		render(<LocalWorkflowProviderRetry retry={retry} />);
		expect(screen.getByText("Waiting for provider access")).toBeVisible();
		expect(screen.getByText("Provider authentication needs recovery")).toBeVisible();
		expect(screen.getByText("Automatic retries: 1 of 3")).toBeVisible();
		expect(screen.getAllByRole("time").map((time) => time.getAttribute("datetime"))).toContain(retry.nextAt);
		expect(screen.queryByRole("button")).toBeNull();
	});
	it.each([
		["checking", "Checking provider access"],
		["exhausted", "Automatic provider recovery stopped"],
		["uncertain", "Provider retry completion is unconfirmed"],
	] as const)("shows %s without promising another scheduled attempt", (state, text) => {
		render(<LocalWorkflowProviderRetry retry={{ ...retry, state }} />);
		expect(screen.getByText(text)).toBeVisible();
		expect(screen.queryByText(/^Next attempt:/)).toBeNull();
		if (state === "exhausted") expect(screen.getByText("Check provider access and resume manually when ready.")).toBeVisible();
		if (state === "uncertain") {
			expect(screen.getByText("End the affected session and confirm that its provider process has ended before starting another task.")).toBeVisible();
			expect(screen.queryByText("Check provider access and resume manually when ready.")).toBeNull();
		}
	});
	it("marks a recovered issue as historical instead of continuing the waiting warning", () => {
		render(<LocalWorkflowProviderRetry retry={{ ...retry, state: "recovered" }} />);
		expect(screen.getByText("Provider access recovered")).toBeVisible();
		expect(screen.getByText("Previous issue: Provider authentication needs recovery")).toBeVisible();
		expect(screen.queryByText("Waiting for provider access")).toBeNull();
		expect(screen.queryByText(/^Next attempt:/)).toBeNull();
	});
	it("keeps historical waiting facts without presenting an old schedule as current", () => {
		render(<LocalWorkflowProviderRetry retry={retry} historical />);
		expect(screen.getByText("Provider recovery history")).toBeVisible();
		expect(screen.getByText("Previous issue: Provider authentication needs recovery")).toBeVisible();
		expect(screen.queryByText(/^Next attempt:/)).toBeNull();
	});
	it.each(["rate_limited", "unavailable"] as const)("uses a safe localized %s reason", (reason) => {
		render(<LocalWorkflowProviderRetry retry={{ ...retry, reason }} />);
		expect(screen.getByText(reason === "rate_limited" ? "Provider rate limit reached" : "Provider is temporarily unavailable")).toBeVisible();
	});
	it("does not render missing retry facts or invalid dates", () => {
		const view = render(<LocalWorkflowProviderRetry />);
		expect(view.container).toBeEmptyDOMElement();
		view.rerender(<LocalWorkflowProviderRetry retry={{ ...retry, failureAt: "invalid", lastAt: "invalid", nextAt: "invalid" }} />);
		expect(screen.queryByRole("time")).toBeNull();
		expect(screen.queryByText(/Invalid Date/)).toBeNull();
	});
	it("does not expose unknown provider text or turn an unknown state into a scheduled retry", () => {
		const unknown = JSON.parse(JSON.stringify({ ...retry, state: "future-state", reason: "raw diagnostic with private content" })) as LocalWorkflowProviderRetryFacts;
		render(<LocalWorkflowProviderRetry retry={unknown} />);
		expect(screen.getByText("Provider retry completion is unconfirmed")).toBeVisible();
		expect(screen.queryByText(/private content/)).toBeNull();
		expect(screen.queryByText(/^Next attempt:/)).toBeNull();
	});
});
