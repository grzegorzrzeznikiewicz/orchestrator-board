import { execFileSync, spawnSync } from "node:child_process";
import { appendFileSync, readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

export const areas = ["backend", "cloud", "frontend", "cli", "mobile", "e2e_gate"];

export function selectChecks(paths) {
	const checks = Object.fromEntries(areas.map((area) => [area, false]));
	const all = () => Object.fromEntries(areas.map((area) => [area, true]));
	if (paths === null) return all();
	for (const path of paths) {
		if (path.startsWith(".github/") || path.startsWith("scripts/ci/") || ["package.json", "package-lock.json"].includes(path)) return all();
		if (["backend/go.mod", "backend/go.sum", "cloud/go.mod", "cloud/go.sum"].includes(path)) {
			// Both modules participate in go.work's dependency-version selection.
			checks.backend = checks.cloud = checks.cli = true;
		} else if (path.startsWith("backend/")) {
			checks.backend = checks.cli = true;
			if (path.startsWith("backend/internal/httpd/")) checks.frontend = checks.mobile = true;
			if (path.startsWith("backend/pkg/")) checks.cloud = checks.frontend = checks.mobile = true;
		} else if (path.startsWith("cloud/") || path.startsWith("contracts/cloud/")) {
			checks.cloud = checks.frontend = checks.mobile = true;
		} else if (path === "frontend/src/api/schema.ts") {
			checks.backend = checks.frontend = checks.mobile = true;
		} else if (path.startsWith("frontend/")) {
			checks.frontend = true;
		} else if (path.startsWith("packages/product-ui/") || path.startsWith("packages/cloud-client/")) {
			checks.frontend = checks.mobile = true;
		} else if (path.startsWith("packages/mobile/")) {
			checks.mobile = true;
		} else if (path.startsWith("test/cli/")) {
			checks.cli = true;
		} else if (path.startsWith("test/e2e-pod/") || /^scripts\/ao-e2e-pod-gate(?:\.test)?\.mjs$/.test(path)) {
			checks.e2e_gate = true;
		} else if (path.startsWith("pricing/")) {
			checks.backend = true;
		} else if (path.startsWith("docs/") || path.startsWith("translations/") || /^[^/]+\.md$/.test(path)) {
			// Documentation has no executable inputs to the test suites.
		} else {
			// New/unclassified source directories must not silently lose coverage.
			return all();
		}
	}
	return checks;
}

export function selectBase(eventName, event) {
	const sha = eventName === "pull_request" ? event.pull_request?.base?.sha : eventName === "push" ? event.before : null;
	if (!sha || /^0+$/.test(sha)) return null;
	if (!/^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(sha)) throw new Error("Invalid base SHA");
	return sha;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
	const event = JSON.parse(readFileSync(process.env.GITHUB_EVENT_PATH, "utf8"));
	const base = selectBase(process.env.GITHUB_EVENT_NAME, event);
	let paths = null;
	if (base && spawnSync("git", ["cat-file", "-e", `${base}^{commit}`]).status === 0) {
		// No API file-count limit; --no-renames includes BOTH sides of moves.
		paths = execFileSync("git", ["diff", "--no-renames", "--name-only", "-z", base, "HEAD"], { encoding: "utf8" }).split("\0").filter(Boolean);
	}
	const checks = selectChecks(paths);
	console.log(JSON.stringify({ changedFiles: paths?.length ?? "full check", checks }, null, 2));
	appendFileSync(process.env.GITHUB_OUTPUT, Object.entries(checks).map(([key, value]) => `${key}=${value}\n`).join(""));
	if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `### Selected checks\n\n${Object.entries(checks).map(([key, value]) => `- ${key}: ${value ? "required" : "unaffected"}`).join("\n")}\n`);
}
