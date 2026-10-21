// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

// @vitest-environment node
import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, expect, it } from "vitest";
import { consumeUpdateRelaunchFlag, markUpdateRelaunch } from "./update-relaunch-flag";

let root: string;
const markerFile = (dir: string) => path.join(dir, "orchestrator-board-update-restart", "relaunch-flag.json");

beforeEach(async () => {
	root = await mkdtemp(path.join(os.tmpdir(), "ao-relaunch-flag-"));
});
afterEach(async () => {
	await rm(root, { recursive: true, force: true });
});

it("returns true once for a genuine post-update relaunch of the same version", async () => {
	await markUpdateRelaunch({ stateDir: root, version: "1.2.3", pid: 100, now: 1_000 });
	const first = await consumeUpdateRelaunchFlag({ stateDir: root, version: "1.2.3", pid: 200, now: 2_000 });
	expect(first).toBe(true);
	// Consumed: a subsequent normal launch reads false.
	const second = await consumeUpdateRelaunchFlag({ stateDir: root, version: "1.2.3", pid: 200, now: 2_000 });
	expect(second).toBe(false);
});

it("returns false when the relaunched build reports a different version", async () => {
	await markUpdateRelaunch({ stateDir: root, version: "1.2.3", pid: 100, now: 1_000 });
	expect(await consumeUpdateRelaunchFlag({ stateDir: root, version: "1.2.2", pid: 200, now: 2_000 })).toBe(false);
});

it("returns false and consumes a marker older than its lifetime", async () => {
	await markUpdateRelaunch({ stateDir: root, version: "1.2.3", pid: 100, now: 0 });
	const stale = 31 * 60_000;
	expect(await consumeUpdateRelaunchFlag({ stateDir: root, version: "1.2.3", pid: 200, now: stale })).toBe(false);
	await expect(readFile(markerFile(root), "utf8")).rejects.toThrow();
});

it("returns false when there is no marker", async () => {
	expect(await consumeUpdateRelaunchFlag({ stateDir: root, version: "1.2.3", pid: 200, now: 2_000 })).toBe(false);
});

it("never throws on a marker that is not a JSON object", async () => {
	for (const contents of ["null", "42", '"oops"', "{ truncated", ""]) {
		await mkdir(path.join(root, "orchestrator-board-update-restart"), { recursive: true });
		await writeFile(markerFile(root), contents);
		await expect(consumeUpdateRelaunchFlag({ stateDir: root, version: "1.2.3", pid: 200, now: 2_000 })).resolves.toBe(
			false,
		);
		await expect(readFile(markerFile(root), "utf8")).rejects.toThrow();
	}
});


it("does not consume the original application's relaunch marker", async () => {
  const legacyDir = path.join(root, "update-restart");
  const legacy = path.join(legacyDir, "relaunch-flag.json");
  await mkdir(legacyDir);
  const contents = JSON.stringify({ version: "1.2.3", fromPID: 100, startedAt: 1_000 });
  await writeFile(legacy, contents);
  expect(await consumeUpdateRelaunchFlag({ stateDir: root, version: "1.2.3", now: 2_000 })).toBe(false);
  expect(await readFile(legacy, "utf8")).toBe(contents);
  await markUpdateRelaunch({ stateDir: root, version: "1.2.4", now: 2_000 });
  expect(JSON.parse(await readFile(markerFile(root), "utf8")).version).toBe("1.2.4");
  expect(await readFile(legacy, "utf8")).toBe(contents);
});
