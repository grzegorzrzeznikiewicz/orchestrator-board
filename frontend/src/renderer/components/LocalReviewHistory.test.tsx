// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

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
	it("opens the newest round when timestamps are equal", () => {
		render(<LocalReviewHistory runs={[{...approved,id:"old",round:1,body:"Older round body"},{...approved,id:"new",round:2,body:"Newest round body"}]} />);
		expect(screen.getByText("Newest round body")).toBeVisible();
		expect(screen.getByText("Older round body")).not.toBeVisible();
	});
	it("shows round, locations and separates required changes from suggestions", () => {
		render(<LocalReviewHistory runs={[{...approved,round:2,verdict:"changes_requested",localResult:{version:1,snapshotId:approved.snapshotId!,round:2,outcome:"changes_requested",summary:"Validation missing",findings:[{kind:"required",path:"new.go",line:4,reason:"Invalid input is saved"},{kind:"suggestion",path:"new.go",line:8,reason:"Consider a clearer name"}]}}]} />);
		expect(screen.getByText("Round 2")).toBeVisible();
		expect(screen.getByText("Required change")).toBeVisible();
		expect(screen.getByText("Suggestion")).toBeVisible();
		expect(screen.getByText("new.go:4")).toBeVisible();
		expect(screen.getByText("Invalid input is saved")).toBeVisible();
	});
	it("shows a failed review even after its snapshot becomes stale", () => {
		render(<LocalReviewHistory runs={[{...approved,current:false,status:"failed",verdict:"",body:"Claude timed out"}]} />);
		expect(screen.getByText("Failed")).toBeVisible();
		expect(screen.queryByText("Reviewer approved")).not.toBeInTheDocument();
		expect(screen.getByText("Claude timed out")).toBeVisible();
	});
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
