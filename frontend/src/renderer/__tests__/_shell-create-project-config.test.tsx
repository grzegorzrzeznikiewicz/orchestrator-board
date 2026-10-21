// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { describe, expect, it } from "vitest";
import { createProjectConfig } from "../routes/_shell";

describe("createProjectConfig", () => {
	it("persists selected PR worker and orchestrator agents without tracker intake", () => {
		expect(
			createProjectConfig({
				reviewMode: "pr",
				workerAgent: "codex",
				orchestratorAgent: "claude-code",
			}),
		).toEqual({
			reviewMode: "pr",
			worker: { agent: "codex" },
			orchestrator: { agent: "claude-code" },
		});
	});

	it("defaults a new local project to Codex execution and independent Claude review", () => {
		expect(createProjectConfig({ workerAgent: "codex", orchestratorAgent: "claude-code", defaultBranch: "main" })).toEqual({
			defaultBranch: "main", worker: { agent: "codex" }, orchestrator: { agent: "claude-code" },
			reviewMode: "local", reviewers: [{ harness: "claude-code" }], autoReview: false, workersRequestReview: false,
		});
	});

	it("keeps explicit PR creation on the selected agents without local workflow settings", () => {
		expect(createProjectConfig({ workerAgent: "cursor", orchestratorAgent: "opencode", reviewMode: "pr" })).toEqual({
			reviewMode: "pr",
			worker: { agent: "cursor" }, orchestrator: { agent: "opencode" },
		});
	});

	it("preserves remote creation defaults without imposing the local workflow", () => {
		expect(createProjectConfig({ workerAgent: "opencode", orchestratorAgent: "opencode" }, "remote")).toEqual({
			worker: { agent: "opencode" }, orchestrator: { agent: "opencode" },
		});
	});

	it("preserves tracker intake alongside selected agent defaults", () => {
		expect(
			createProjectConfig({
				reviewMode: "pr",
				workerAgent: "cursor",
				orchestratorAgent: "opencode",
				trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
			}),
		).toEqual({
			reviewMode: "pr",
			worker: { agent: "cursor" },
			orchestrator: { agent: "opencode" },
			trackerIntake: { enabled: true, provider: "github", assignee: "octocat" },
		});
	});
});
