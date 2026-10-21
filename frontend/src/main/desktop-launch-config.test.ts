// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).
// @vitest-environment node

import { mkdir, mkdtemp, readFile, readdir, rm, symlink, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, describe, expect, it } from "vitest";
import { readDesktopLaunchConfig } from "./desktop-launch-config";
import { resolveElectronUserData } from "./electron-user-data";
import { resolveDesktopDataDir } from "./telemetry-policy-file";

const temporaryDirectories: string[] = [];
afterEach(async () => {
	await Promise.all(temporaryDirectories.splice(0).map((directory) => rm(directory, { recursive: true, force: true })));
});

async function fixture() {
	const homeDir = await mkdtemp(path.join(os.tmpdir(), "ob-desktop-launch-"));
	temporaryDirectories.push(homeDir);
	return {
		homeDir,
		file: path.join(homeDir, ".ao", "orchestrator-board", "desktop-launch.json"),
		config: {
			version: 1,
			dataDir: path.join(homeDir, ".ao", "chosen", "data"),
			runFile: path.join(homeDir, ".ao", "chosen", "running.json"),
			port: 3917,
		},
	};
}
async function put(file: string, contents: unknown) {
	await mkdir(path.dirname(file), { recursive: true });
	await writeFile(file, typeof contents === "string" ? contents : JSON.stringify(contents));
}

describe("packaged fork launch configuration", () => {
	it("keeps existing default data and Chromium paths when no configuration exists, without creating state", async () => {
		const { homeDir } = await fixture();
		const env = { ...readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} }) };
		const desktopDataDir = resolveDesktopDataDir(env, homeDir, "/", true);
		expect(desktopDataDir).toBe(path.join(homeDir, ".ao", "data"));
		expect(resolveElectronUserData({ env, homeDir, desktopDataDir, isPackaged: true })).toBe(path.join(homeDir, ".ao", "orchestrator-board", "electron"));
		expect(await readdir(homeDir)).toEqual([]);
	});

	it("keeps defaults when the configuration is genuinely absent under an existing directory", async () => {
		const { homeDir, file } = await fixture();
		await mkdir(path.dirname(file), { recursive: true });
		expect(readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toEqual({});
	});

	it("fails closed for a dangling configuration-file symlink", async () => {
		const { homeDir, file } = await fixture();
		await mkdir(path.dirname(file), { recursive: true });
		await symlink(path.join(homeDir, ".ao", "missing-config.json"), file, "file");
		expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toThrow(/desktop-launch\.json/);
	});

	it("fails closed for a dangling configuration-directory symlink", async () => {
		const { homeDir, file } = await fixture();
		await mkdir(path.join(homeDir, ".ao"));
		await symlink(path.join(homeDir, ".ao", "missing-directory"), path.dirname(file), process.platform === "win32" ? "junction" : "dir");
		expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toThrow(/desktop-launch\.json/);
	});

	it("restores the selected data, daemon handshake and Chromium profile for an ordinary launch", async () => {
		const { homeDir, file, config } = await fixture();
		await put(file, config);
		const env = { ...readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} }) };
		expect(env).toEqual({ AO_DATA_DIR: config.dataDir, AO_RUN_FILE: config.runFile, AO_PORT: "3917" });
		const desktopDataDir = resolveDesktopDataDir(env, homeDir, "/", true);
		expect(resolveElectronUserData({ env, homeDir, desktopDataDir, isPackaged: true })).toBe(path.join(config.dataDir, "orchestrator-board", "electron"));
	});

	it("never reads stored configuration in an unpackaged run", async () => {
		const { homeDir, file } = await fixture();
		await put(file, "malformed JSON");
		expect(readDesktopLaunchConfig({ homeDir, isPackaged: false, env: {} })).toEqual({});
	});

	it("bypasses the complete stored configuration when AO_DATA_DIR explicitly selects a run", async () => {
		const { homeDir, file } = await fixture();
		await put(file, "malformed JSON");
		const env = { AO_DATA_DIR: " /explicit/task-data ", AO_RUN_FILE: "/explicit/running.json", AO_PORT: "4210" };
		expect(readDesktopLaunchConfig({ homeDir, isPackaged: true, env })).toEqual({});
		expect(env).toEqual({ AO_DATA_DIR: " /explicit/task-data ", AO_RUN_FILE: "/explicit/running.json", AO_PORT: "4210" });
	});

	it("treats blank AO_DATA_DIR as absent and preserves explicit run-file and port overrides", async () => {
		const { homeDir, file, config } = await fixture();
		await put(file, config);
		const env = { AO_DATA_DIR: "  ", AO_RUN_FILE: "/explicit/running.json", AO_PORT: "4210", OTHER_SETTING: "kept" };
		expect(readDesktopLaunchConfig({ homeDir, isPackaged: true, env })).toEqual({ AO_DATA_DIR: config.dataDir });
		expect(env).toEqual({ AO_DATA_DIR: "  ", AO_RUN_FILE: "/explicit/running.json", AO_PORT: "4210", OTHER_SETTING: "kept" });
	});

	it("does not write configuration, create the selected data directory or alter input environment", async () => {
		const { homeDir, file, config } = await fixture();
		await put(file, config);
		const original = await readFile(file, "utf8");
		const before = await readdir(homeDir, { recursive: true });
		const env = {};
		readDesktopLaunchConfig({ homeDir, isPackaged: true, env });
		expect(await readdir(homeDir, { recursive: true })).toEqual(before);
		expect(await readFile(file, "utf8")).toBe(original);
		expect(env).toEqual({});
	});

	it.each([
		["invalid JSON", "{"],
		["non-object", "null"],
		["array", "[]"],
	])("fails closed for %s instead of selecting the default database", async (_name, contents) => {
		const { homeDir, file } = await fixture();
		await put(file, contents);
		expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toThrow(/desktop-launch\.json/);
	});

	it.each([
		["wrong version", { version: 2 }],
		["string version", { version: "1" }],
		["unknown key", { AO_AGENT_BROWSER_PATH: "/arbitrary/executable" }],
		["nested environment", { env: { AO_PORT: "4000" } }],
		["port zero", { port: 0 }],
		["port above range", { port: 65536 }],
		["fractional port", { port: 3917.5 }],
		["string port", { port: "3917" }],
	])("rejects %s", async (_name, patch) => {
		const { homeDir, file, config } = await fixture();
		await put(file, { ...config, ...patch });
		expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toThrow(/desktop-launch\.json/);
	});

	it.each(["dataDir", "runFile", "port"])("requires %s to keep the launch selection complete", async (field) => {
		const { homeDir, file, config } = await fixture();
		const incomplete: Record<string, unknown> = { ...config };
		delete incomplete[field];
		await put(file, incomplete);
		expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toThrow(/desktop-launch\.json/);
	});

	it.each(["dataDir", "runFile"])("requires %s to be an absolute path contained in AO state", async (field) => {
		const { homeDir, file, config } = await fixture();
		for (const invalid of ["relative/path", "", "/outside/state", path.join(homeDir, ".ao-other", "file"), path.join(homeDir, ".ao", "..", "escaped"), config.dataDir + "\0suffix"]) {
			await put(file, { ...config, [field]: invalid });
			expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} }), invalid).toThrow(/desktop-launch\.json/);
		}
	});

	it.each(["dataDir", "runFile"])("rejects a %s symlink escaping AO state", async (field) => {
		const { homeDir, file, config } = await fixture();
		const outside = path.join(homeDir, "outside");
		await mkdir(outside);
		await mkdir(path.join(homeDir, ".ao"));
		await symlink(outside, path.join(homeDir, ".ao", "escape"), process.platform === "win32" ? "junction" : "dir");
		await put(file, { ...config, [field]: path.join(homeDir, ".ao", "escape", "target") });
		expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toThrow(/desktop-launch\.json/);
	});

	it.each(["dataDir", "runFile"])("rejects a dangling ancestor of the selected %s", async (field) => {
		const { homeDir, file, config } = await fixture();
		await mkdir(path.join(homeDir, ".ao"));
		await symlink(path.join(homeDir, "missing-outside-state"), path.join(homeDir, ".ao", "broken-selection"), process.platform === "win32" ? "junction" : "dir");
		await put(file, { ...config, [field]: path.join(homeDir, ".ao", "broken-selection", "future") });
		expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toThrow(/desktop-launch\.json/);
	});

	it("rejects paths outside AO state even when they alias an internal directory", async () => {
		const { homeDir, file, config } = await fixture();
		await mkdir(config.dataDir, { recursive: true });
		const alias = path.join(homeDir, "outside-alias");
		await symlink(config.dataDir, alias, process.platform === "win32" ? "junction" : "dir");
		await put(file, { ...config, dataDir: alias });
		expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toThrow(/desktop-launch\.json/);
	});

	it("reports a present unreadable configuration instead of falling back", async () => {
		const { homeDir, file } = await fixture();
		await mkdir(file, { recursive: true });
		expect(() => readDesktopLaunchConfig({ homeDir, isPackaged: true, env: {} })).toThrow(/desktop-launch\.json/);
	});
});
