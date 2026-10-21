// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { lstatSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { canonicalPathInside } from "../shared/path-identity";

type DesktopLaunchOptions = {
	env: Record<string, string | undefined>;
	homeDir: string;
	isPackaged: boolean;
};

type LaunchEnvironment = Partial<Record<"AO_DATA_DIR" | "AO_RUN_FILE" | "AO_PORT", string>>;

// Missing components are valid for a future data directory. Existing links
// must resolve, so a dangling link cannot be mistaken for an absent selection.
function assertResolvableLinks(target: string, stateDir: string): void {
	let candidate = stateDir;
	for (const component of ["", ...path.relative(stateDir, target).split(path.sep)]) {
		candidate = path.join(candidate, component);
		const entry = lstatSync(candidate, { throwIfNoEntry: false });
		if (!entry) return;
		if (entry.isSymbolicLink()) statSync(candidate);
	}
}

/** Read an explicitly saved fork launch selection; never create or migrate state. */
export function readDesktopLaunchConfig({ env, homeDir, isPackaged }: DesktopLaunchOptions): LaunchEnvironment {
	// A task-specific data directory must not inherit the installed app's daemon
	// handshake or port. Developer launches retain their existing isolation too.
	if (!isPackaged || env.AO_DATA_DIR?.trim()) return {};
	const stateDir = path.join(homeDir, ".ao");
	const file = path.join(stateDir, "orchestrator-board", "desktop-launch.json");
	const invalid = (reason: string) => new Error(`Cannot use launch configuration ${file}: ${reason}`);
	try { assertResolvableLinks(file, stateDir); }
	catch { throw invalid("the saved path could not be resolved."); }
	if (!canonicalPathInside(file, stateDir)) throw invalid("the file must remain inside ~/.ao.");
	let raw: string;
	try {
		raw = readFileSync(file, "utf8");
	} catch (error) {
		if ((error as NodeJS.ErrnoException).code === "ENOENT") return {};
		throw invalid("the saved file could not be read.");
	}
	let parsed: unknown;
	try { parsed = JSON.parse(raw); }
	catch { throw invalid("the saved file is not valid JSON."); }
	if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) throw invalid("expected a version 1 launch selection.");
	const config = parsed as Record<string, unknown>;
	if (config.version !== 1) throw invalid("only version 1 is supported.");
	if (Object.keys(config).some((key) => !["version", "dataDir", "runFile", "port"].includes(key))) throw invalid("the saved file contains unsupported settings.");
	const selectedPath = (field: "dataDir" | "runFile"): string => {
		const value = config[field];
		if (typeof value !== "string" || !value.trim() || value.includes("\0") || !path.isAbsolute(value.trim())) throw invalid(`${field} must be an absolute path inside ~/.ao.`);
		const resolved = path.resolve(value.trim());
		const relative = path.relative(stateDir, resolved);
		if (relative === ".." || relative.startsWith(".." + path.sep) || path.isAbsolute(relative)) throw invalid(`${field} must remain inside ~/.ao.`);
		try { assertResolvableLinks(resolved, stateDir); }
		catch { throw invalid(`${field} contains an unavailable path.`); }
		if (!canonicalPathInside(resolved, stateDir)) throw invalid(`${field} must remain inside ~/.ao.`);
		return resolved;
	};
	const dataDir = selectedPath("dataDir");
	const runFile = selectedPath("runFile");
	if (typeof config.port !== "number" || !Number.isInteger(config.port) || config.port < 1 || config.port > 65535) throw invalid("port must be an integer between 1 and 65535.");
	return {
		AO_DATA_DIR: dataDir,
		...(!env.AO_RUN_FILE?.trim() ? { AO_RUN_FILE: runFile } : {}),
		...(!env.AO_PORT?.trim() ? { AO_PORT: String(config.port) } : {}),
	};
}
