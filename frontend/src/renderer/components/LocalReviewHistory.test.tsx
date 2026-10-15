import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { LocalReviewHistory } from "./LocalReviewHistory";
import type { ReviewRunFacts } from "../lib/session-reviews";

afterEach(cleanup);
const approved: ReviewRunFacts = {
	id: "local-run", reviewId: "review", sessionId: "worker", harness: "claude-code",
	prUrl: "", targetSha: "", targetKind: "local", snapshotId: "a".repeat(64),
	batchId:"batch", githubReviewId:"",autoInjectReview:false,triggerSource:"manual",
 status: "complete", verdict: "approved", body: "Historical review text",
	createdAt: "2026-10-15T12:00:00Z", current: true,
};

describe("LocalReviewHistory", () => {
	it("shows the full snapshot identity and a reviewer verdict", () => {
		render(<LocalReviewHistory runs={[approved]} />);
		expect(screen.getByText("Reviewer approved")).toBeVisible();
		expect(screen.getByText(new RegExp(approved.snapshotId!))).toBeVisible();
		expect(screen.queryByRole("button", { name: /accept/i })).not.toBeInTheDocument();
	});
	it("never presents an invalidated approval as current", () => {
		render(<LocalReviewHistory runs={[{ ...approved, current: false, invalidatedAt: "2026-10-15T12:01:00Z", invalidationReason: "Executor resumed editing" }]} />);
		expect(screen.getByText("Outdated snapshot — run review again")).toBeVisible();
		expect(screen.queryByText("Reviewer approved")).not.toBeInTheDocument();
		expect(screen.getByText("Historical review text")).toBeVisible();
		expect(screen.getByRole("status")).toHaveTextContent("Executor resumed editing");
	});
});
