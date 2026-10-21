// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { useEffect, useMemo, useRef } from "react";
import { useLocation, useNavigate, useParams } from "@tanstack/react-router";
import { getMostRecentlyOpenedProjectId, recordProjectOpened } from "../lib/project-history";
import { CLOUD_PROJECT_KIND, STANDALONE_PROJECT_KIND, STANDALONE_WORKSPACE_ID, type WorkspaceSummary } from "../types/workspace";

/** Restore one startup board while keeping explicit routes and later Home intact. */
export function useProjectNavigationPersistence({ workspaces, workspaceStartupState }: {
	workspaces: WorkspaceSummary[];
	workspaceStartupState: "loading" | "ready" | "error";
}): void {
	const navigate = useNavigate();
	const pathname = useLocation({ select: (location) => location.pathname });
	const { hostId, projectId, sessionId } = useParams({ strict: false }) as { hostId?: string; projectId?: string; sessionId?: string };
	const startupPending = useRef(pathname === "/");
	const localProjects = useMemo(() => workspaces.filter((workspace) => !workspace.hostId && !workspace.folderMissing && workspace.kind !== CLOUD_PROJECT_KIND && workspace.kind !== STANDALONE_PROJECT_KIND && workspace.id !== STANDALONE_WORKSPACE_ID), [workspaces]);
	const selectedProjectId = hostId ? undefined : localProjects.find((workspace) => projectId
		? workspace.id === projectId
		: sessionId && workspace.sessions.some((session) => session.id === sessionId))?.id;

	useEffect(() => {
		if (selectedProjectId) recordProjectOpened(selectedProjectId);
	}, [selectedProjectId]);

	useEffect(() => {
		if (!startupPending.current) return;
		if (pathname !== "/") {
			startupPending.current = false;
			return;
		}
		if (workspaceStartupState !== "ready") return;
		startupPending.current = false;
		const restoredProjectId = getMostRecentlyOpenedProjectId(localProjects.map((workspace) => workspace.id));
		if (restoredProjectId) void navigate({ to: "/projects/$projectId", params: { projectId: restoredProjectId }, replace: true });
	}, [localProjects, navigate, pathname, workspaceStartupState]);
}
