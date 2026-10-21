// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
	createMemoryHistory,
	createRootRoute,
	createRoute,
	createRouter,
	Outlet,
	RouterProvider,
	useNavigate,
} from "@tanstack/react-router";
import { act, cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ShellTopbar } from "./ShellTopbar";
import { SessionsBoard } from "./SessionsBoard";
import { ChatComposer } from "./chat/ChatComposer";
import { TooltipProvider } from "./ui/tooltip";
import { readChatSessionDraft, writeChatComposerText } from "../lib/chat-drafts";
import { getChatDraftBoundaries } from "../lib/chat-draft-boundary";
import { useUiStore } from "../stores/ui-store";

const { getMock, sendMock } = vi.hoisted(() => ({
	getMock: vi.fn(),
	sendMock: vi.fn(),
}));

// Keep the actual router, workspace mapping, board, and durable composer. Only
// the daemon boundary and native notification surface are replaced here.
vi.mock("../lib/api-client", () => ({
	apiClient: { GET: getMock, POST: vi.fn() },
	getApiBaseUrl: () => "http://127.0.0.1:3001",
	subscribeApiBaseUrl: () => () => undefined,
	hasTrustedApiBaseUrl: () => true,
	apiErrorCode: () => undefined,
	apiErrorMessage: (error: unknown, fallback = "Request failed") =>
		error instanceof Error ? error.message : fallback,
}));

vi.mock("./NotificationCenter", () => ({
	NotificationCenter: () => <button aria-label="Notifications" type="button" />,
}));

const projectId = "budget-planner-app";
const sessionId = "budget-planner-app-1";
const sessionPath = `/projects/${projectId}/sessions/${sessionId}`;
const projectPath = `/projects/${projectId}`;
const draft = "Keep this unsent request while I inspect the Kanban board.";
const clients: QueryClient[] = [];

function respondWithProject(reviewMode: "local" | "pr") {
	getMock.mockImplementation(async (path: string) => {
		if (path === "/api/v1/projects") {
			return {
				data: { projects: [{ id: projectId, name: projectId, path: "/repo/budget-planner-app", kind: "single_repo", reviewMode, orchestratorAgent: "claude-code" }] },
			};
		}
		if (path === "/api/v1/sessions") {
			return {
				data: { sessions: [{ id: sessionId, projectId, kind: "orchestrator", mode: "chat", harness: "claude-code", status: "idle", activity: { state: "idle" }, isTerminated: false, createdAt: "2026-10-17T12:00:00Z", updatedAt: "2026-10-17T12:00:00Z", prs: [] }] },
			};
		}
		if (path === "/api/v1/system/requirements") return { data: { ready: true, requirements: [] } };
		if (path === "/api/v1/system/github-auth") return { data: { satisfied: true } };
		if (path === "/api/v1/session-interface-transitions/status") return { data: { status: "ok", transitions: [] } };
		return { data: { status: "ok", sessions: [], reviews: [], harnesses: [], installed: [] } };
	});
}

function renderNavigation() {
	const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } });
	clients.push(queryClient);
	const root = createRootRoute({ component: () => <Outlet /> });
	const sessionRoute = createRoute({
		getParentRoute: () => root,
		path: "/projects/$projectId/sessions/$sessionId",
		component: () => (
			<>
				<ShellTopbar embedded />
				<ChatComposer draftSessionId={sessionId} onSend={sendMock} />
			</>
		),
	});
	const boardRoute = createRoute({
		getParentRoute: () => root,
		path: "/projects/$projectId",
		component: function ProjectBoard() {
			const navigate = useNavigate();
			return <>
				<button type="button" onClick={() => void navigate({ to: "/projects/$projectId/sessions/$sessionId", params: { projectId, sessionId } })}>Return to orchestrator</button>
				<SessionsBoard projectId={projectId} />
			</>;
		},
	});
	const history = createMemoryHistory({ initialEntries: [sessionPath] });
	const router = createRouter({ routeTree: root.addChildren([boardRoute, sessionRoute]), history, defaultPendingMinMs: 0 });
	render(
		<QueryClientProvider client={queryClient}>
			<TooltipProvider><RouterProvider router={router} /></TooltipProvider>
		</QueryClientProvider>,
	);
	return router;
}

describe("Open Kanban with real route navigation", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		localStorage.clear();
		vi.spyOn(window, "scrollTo").mockImplementation(() => undefined);
		useUiStore.setState({ developerMode: false, diagnostics: false });
		writeChatComposerText(sessionId, draft);
	});

	afterEach(() => {
		cleanup();
		for (const client of clients.splice(0)) client.clear();
		vi.restoreAllMocks();
	});

	it.each([
		["local", 6],
		["pr", 4],
	] as const)("opens the %s project board from a chat with a saved unsent draft", async (reviewMode, expectedColumns) => {
		respondWithProject(reviewMode);
		const router = renderNavigation();
		const user = userEvent.setup();
		const composer = await screen.findByLabelText("Message the agent");
		await waitFor(() => expect(composer).toHaveTextContent(draft));
		expect(getChatDraftBoundaries(sessionId)).toEqual([]);

		await user.click(await screen.findByRole("button", { name: "Open Kanban" }));

		await waitFor(() => expect(router.state.location.pathname).toBe(projectPath));
		await waitFor(() => expect(document.querySelectorAll("section[data-column], section[data-kanban-column]")).toHaveLength(expectedColumns));
		expect(screen.queryByLabelText("Message the agent")).not.toBeInTheDocument();
		expect(readChatSessionDraft(sessionId).composer.text).toBe(draft);
		expect(sendMock).not.toHaveBeenCalled();

		await user.click(screen.getByRole("button", { name: "Return to orchestrator" }));
		await waitFor(() => expect(router.state.location.pathname).toBe(sessionPath));
		expect(await screen.findByLabelText("Message the agent")).toHaveTextContent(draft);
		expect(sendMock).not.toHaveBeenCalled();
		await act(async () => router.history.destroy());
	});
});
