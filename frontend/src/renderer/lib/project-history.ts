// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

const PROJECT_HISTORY_KEY = "ao.project-history";

type ProjectHistory = Record<string, string>;

function readHistory(): ProjectHistory {
	if (typeof window === "undefined") return {};
	try {
		const value: unknown = JSON.parse(window.localStorage.getItem(PROJECT_HISTORY_KEY) ?? "{}");
		if (!value || typeof value !== "object" || Array.isArray(value)) return {};
		return Object.fromEntries(
			Object.entries(value).filter((entry): entry is [string, string] => typeof entry[0] === "string" && typeof entry[1] === "string"),
		);
	} catch {
		return {};
	}
}

export function getProjectLastOpenedAt(projectId: string): string | undefined {
	return readHistory()[projectId];
}

/** Explicit project selections only; worker activity never chooses startup. */
export function getMostRecentlyOpenedProjectId(projectIds: readonly string[]): string | undefined {
	const available = new Set(projectIds);
	let selected: string | undefined;
	let latest = -Infinity;
	for (const [projectId, openedAt] of Object.entries(readHistory())) {
		const timestamp = Date.parse(openedAt);
		if (available.has(projectId) && Number.isFinite(timestamp) && timestamp >= latest) {
			selected = projectId;
			latest = timestamp;
		}
	}
	return selected;
}

export function recordProjectOpened(projectId: string): void {
	if (typeof window === "undefined") return;
	try {
		window.localStorage.setItem(PROJECT_HISTORY_KEY, JSON.stringify({ ...readHistory(), [projectId]: new Date().toISOString() }));
	} catch {
		// Local history is best effort and must never block navigation.
	}
}
