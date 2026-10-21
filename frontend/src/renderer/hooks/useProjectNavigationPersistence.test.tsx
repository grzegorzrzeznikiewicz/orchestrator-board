// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { useState, type Dispatch, type SetStateAction } from "react";
import { createMemoryHistory, createRootRoute, createRoute, createRouter, Outlet, RouterProvider } from "@tanstack/react-router";
import { act, cleanup, render, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useProjectNavigationPersistence } from "./useProjectNavigationPersistence";
import { getProjectLastOpenedAt } from "../lib/project-history";
import { STANDALONE_WORKSPACE_ID, type WorkspaceSession, type WorkspaceSummary } from "../types/workspace";

type State = { workspaces: WorkspaceSummary[]; workspaceStartupState: "loading" | "ready" | "error" };
const histories: ReturnType<typeof createMemoryHistory>[] = [];
const project = (id: string, extra: Partial<WorkspaceSummary> = {}): WorkspaceSummary => ({ id, name: id, path: `/repos/${id}`, kind: "single_repo", sessions: [], ...extra });
const worker = (id: string, workspaceId: string): WorkspaceSession => ({ id, workspaceId, workspaceName: workspaceId, title: id, provider: "codex", kind: "worker", mode: "chat", status: "working", prs: [], createdAt: "2026-01-01T00:00:00Z", updatedAt: "2099-01-01T00:00:00Z" });
const projects = [project("budget"), project("other", { sessions: [worker("other-worker", "other")] })];
const openedBudget = "2026-10-17T12:00:00Z";
const openedOther = "2026-10-16T12:00:00Z";

async function start(options: Partial<State> & { entries?: string[] } = {}) {
	let setState!: Dispatch<SetStateAction<State>>;
	const root = createRootRoute({ component: function TestShell() {
		const [state, update] = useState<State>({ workspaces: options.workspaces ?? projects, workspaceStartupState: options.workspaceStartupState ?? "ready" });
		setState = update;
		useProjectNavigationPersistence(state);
		return <Outlet />;
	} });
	const routes = ["/", "/previous", "/settings", "/projects/$projectId", "/projects/$projectId/sessions/$sessionId", "/sessions/$sessionId", "/host/$hostId/project/$projectId"].map((path) => createRoute({ getParentRoute: () => root, path, component: () => <main>Current page</main> }));
	const history = createMemoryHistory({ initialEntries: options.entries ?? ["/"] });
	histories.push(history);
	const router = createRouter({ routeTree: root.addChildren(routes), history, defaultPendingMinMs: 0 });
	const view = render(<RouterProvider router={router} />);
	await act(async () => { await router.load(); });
	return {
		router, history,
		update: async (patch: Partial<State>) => { await act(async () => { setState((state) => ({ ...state, ...patch })); }); },
		unmount: () => view.unmount(),
	};
}

beforeEach(() => {
	localStorage.clear();
	vi.spyOn(window, "scrollTo").mockImplementation(() => undefined);
});
afterEach(() => {
	cleanup();
	for (const history of histories.splice(0)) history.destroy();
	vi.restoreAllMocks();
});
function saveHistory(history: Record<string, unknown>) { localStorage.setItem("ao.project-history", JSON.stringify(history)); }

describe("startup project Kanban and explicit selection history", () => {
	it("restores the last selected project rather than the most active session and replaces startup Home", async () => {
		saveHistory({ budget: openedBudget, other: openedOther });
		const { router, history } = await start({ entries: ["/previous", "/"] });
		await waitFor(() => expect(router.state.location.pathname).toBe("/projects/budget"));
		expect(history.length).toBe(2);
		await act(async () => { history.back(); });
		await waitFor(() => expect(router.state.location.pathname).toBe("/previous"));
	});

	it("waits through loading and error until the confirmed workspace startup is ready", async () => {
		saveHistory({ budget: openedBudget });
		const { router, update } = await start({ workspaces: [], workspaceStartupState: "loading" });
		expect(router.state.location.pathname).toBe("/");
		await update({ workspaces: projects, workspaceStartupState: "error" });
		expect(router.state.location.pathname).toBe("/");
		await update({ workspaceStartupState: "ready" });
		await waitFor(() => expect(router.state.location.pathname).toBe("/projects/budget"));
	});

	it("does not hijack navigation away and back to Home while startup data is loading", async () => {
		saveHistory({ budget: openedBudget });
		const { router, update } = await start({ workspaceStartupState: "loading" });
		await act(async () => { await router.navigate({ to: "/settings" }); });
		await act(async () => { await router.navigate({ to: "/" }); });
		await update({ workspaceStartupState: "ready" });
		expect(router.state.location.pathname).toBe("/");
	});

	it.each(["/projects/other/sessions/other-worker", "/settings", "/host/remote/project/other"])("preserves the explicit route %s and subsequent intentional Home", async (path) => {
		saveHistory({ budget: openedBudget });
		const { router, update } = await start({ entries: [path] });
		expect(router.state.location.pathname).toBe(path);
		await act(async () => { await router.navigate({ to: "/" }); });
		await update({ workspaces: [...projects] });
		expect(router.state.location.pathname).toBe("/");
	});

	it("ignores deleted, missing-folder, remote, cloud and standalone history entries", async () => {
		saveHistory({ budget: openedBudget, deleted: "2099-01-01T00:00:00Z", missing: "2098-01-01T00:00:00Z", remote: "2097-01-01T00:00:00Z", cloud: "2096-01-01T00:00:00Z", [STANDALONE_WORKSPACE_ID]: "2095-01-01T00:00:00Z" });
		const { router } = await start({ workspaces: [...projects, project("missing", { folderMissing: true }), project("remote", { hostId: "remote" }), project("cloud", { kind: "cloud" }), project(STANDALONE_WORKSPACE_ID, { kind: "standalone" })] });
		await waitFor(() => expect(router.state.location.pathname).toBe("/projects/budget"));
	});

	it("keeps Home without valid explicit history even when worker activity is recent", async () => {
		saveHistory({ budget: "not-a-date", other: 42, deleted: openedBudget });
		const { router, update } = await start();
		await update({ workspaces: [...projects] });
		expect(router.state.location.pathname).toBe("/");
	});

	it("records a generic session owner and opens that project's Kanban on the next launch", async () => {
		saveHistory({ budget: "2000-01-01T00:00:00Z" });
		const first = await start({ entries: ["/sessions/other-worker"] });
		await waitFor(() => expect(getProjectLastOpenedAt("other")).toBeDefined());
		first.unmount();
		const next = await start();
		await waitFor(() => expect(next.router.state.location.pathname).toBe("/projects/other"));
	});

	it("does not overwrite project selection history on background workspace refreshes", async () => {
		const { update } = await start({ entries: ["/projects/budget"] });
		await waitFor(() => expect(getProjectLastOpenedAt("budget")).toBeDefined());
		// Another selection/history writer must survive unrelated workspace CDC.
		saveHistory({ budget: openedOther });
		await update({ workspaces: projects.map((workspace) => ({ ...workspace, sessions: workspace.sessions.map((session) => ({ ...session, updatedAt: "2100-01-01T00:00:00Z" })) })) });
		expect(getProjectLastOpenedAt("budget")).toBe(openedOther);
	});

	it("does not persist unknown, remote, cloud or standalone selections", async () => {
		saveHistory({ budget: openedBudget });
		const { router } = await start({ entries: ["/projects/deleted"], workspaces: [...projects, project("remote", { hostId: "remote" }), project("cloud", { kind: "cloud" }), project(STANDALONE_WORKSPACE_ID, { kind: "standalone" })] });
		for (const projectId of ["remote", "cloud", STANDALONE_WORKSPACE_ID]) {
			await act(async () => { await router.navigate({ to: "/projects/$projectId", params: { projectId } }); });
		}
		expect(JSON.parse(localStorage.getItem("ao.project-history") ?? "{}")).toEqual({ budget: openedBudget });
	});
});
