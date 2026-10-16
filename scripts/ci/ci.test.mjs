import assert from "node:assert/strict";
import { test } from "node:test";
import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { selectChecks, selectBase } from "./changes.mjs";
import { checkRequired } from "./required.mjs";

const areas = ["backend", "cloud", "frontend", "cli", "mobile", "e2e_gate"];
const enabled = (paths) => Object.entries(selectChecks(paths)).filter(([, value]) => value).map(([key]) => key);

for (const [name, paths, expected] of [
	["documentation", ["README.md", "docs/ci.md", "docs/screenshots/example.jpg"], []],
	["backend", ["backend/internal/service/review.go"], ["backend", "cli"]],
	["backend public packages used by Cloud", ["backend/pkg/contract/contract.go"], ["backend", "cloud", "frontend", "cli", "mobile"]],
	["backend module requirements", ["backend/go.mod", "backend/go.sum"], ["backend", "cloud", "cli"]],
	["Cloud workspace module requirements", ["cloud/go.mod", "cloud/go.sum"], ["backend", "cloud", "cli"]],
	["API producers", ["backend/internal/httpd/controllers/dto.go"], ["backend", "frontend", "cli", "mobile"]],
	["API consumers", ["frontend/src/api/schema.ts"], ["backend", "frontend", "mobile"]],
	["frontend", ["frontend/src/renderer/App.tsx"], ["frontend"]],
	["docs app", ["frontend/src/docs/package-lock.json"], ["frontend"]],
	["cloud", ["cloud/internal/httpd/routes.go"], ["cloud", "frontend", "mobile"]],
	["cloud contract", ["contracts/cloud/openapi.yaml"], ["cloud", "frontend", "mobile"]],
	["shared client", ["packages/cloud-client/src/client.ts"], ["frontend", "mobile"]],
	["shared UI", ["packages/product-ui/src/index.ts"], ["frontend", "mobile"]],
	["mobile", ["packages/mobile/src/client.ts"], ["mobile"]],
	["CLI fixtures", ["test/cli/install-check.sh"], ["cli"]],
	["pod harness", ["test/e2e-pod/boot-real.sh"], ["e2e_gate"]],
	["pod runner", ["scripts/ao-e2e-pod-gate.mjs"], ["e2e_gate"]],
	["pricing", ["pricing/catalog/v1/index.json"], ["backend"]],
	["root lockfile", ["package-lock.json"], areas],
	["workflow", [".github/workflows/frontend.yml"], areas],
	["CI selector", ["scripts/ci/changes.mjs"], areas],
	["unknown executable", ["scripts/new-build.sh"], areas],
	["missing diff", null, areas],
	["empty diff", [], []],
	["deleted/renamed API source", ["backend/internal/httpd/old.go", "docs/old.go"], ["backend", "frontend", "cli", "mobile"]],
]) {
	test(`selects checks for ${name}`, () => assert.deepEqual(enabled(paths), expected));
}

test("uses the PR base, push predecessor or full-check fallback", () => {
	const sha = "a".repeat(40);
	assert.equal(selectBase("pull_request", { pull_request: { base: { sha } } }), sha);
	assert.equal(selectBase("push", { before: sha }), sha);
	assert.equal(selectBase("push", { before: "0".repeat(40) }), null);
	assert.equal(selectBase("workflow_dispatch", {}), null);
	assert.equal(selectBase("pull_request", {}), null);
	assert.throws(() => selectBase("push", { before: "--malicious-option" }), /Invalid base SHA/);
});

function results(paths) {
	const checks = selectChecks(paths);
	return {
		changes: { result: "success", outputs: Object.fromEntries(Object.entries(checks).map(([key, value]) => [key, String(value)])) },
		workflow: { result: "success" },
		secrets: { result: "success" },
		go: { result: checks.backend || checks.cloud ? "success" : "skipped" },
		frontend: { result: checks.frontend ? "success" : "skipped" },
		cli: { result: checks.cli ? "success" : "skipped" },
		mobile: { result: checks.mobile ? "success" : "skipped" },
		e2e_gate: { result: checks.e2e_gate ? "success" : "skipped" },
	};
}

test("allows docs-only PRs and all-success code PRs", () => {
	assert.deepEqual(checkRequired(results(["docs/ci.md"])), []);
	assert.deepEqual(checkRequired(results(null)), []);
});

for (const job of ["changes", "workflow", "secrets", "go", "frontend", "cli", "mobile", "e2e_gate"]) {
	for (const result of ["failure", "cancelled", "skipped", undefined]) {
		test(`rejects ${job} ${result ?? "missing"} when required`, () => {
			const needs = results(null);
			needs[job] = result ? { ...needs[job], result } : undefined;
			assert.notDeepEqual(checkRequired(needs), []);
		});
	}
}

test("does not hide unexpected failures behind a docs-only selection", () => {
	const needs = results(["README.md"]);
	needs.go.result = "failure";
	assert.notDeepEqual(checkRequired(needs), []);
});

for (const value of [undefined, "", "maybe", true]) {
	test(`rejects invalid or missing selector output ${String(value)}`, () => {
		const needs = results(["README.md"]);
		needs.changes.outputs.backend = value;
		assert.notDeepEqual(checkRequired(needs), []);
	});
}

test("rejects missing result data", () => {
	assert.notDeepEqual(checkRequired({}), []);
});

test("required CLI exits nonzero for a failed or malformed result", () => {
	const script = fileURLToPath(new URL("./required.mjs", import.meta.url));
	for (const [data, status] of [[results(null), 0], [{}, 1], [results(["docs/ci.md"]), 0]]) {
		const run = spawnSync(process.execPath, [script], { env: { ...process.env, CI_NEEDS: JSON.stringify(data), GITHUB_STEP_SUMMARY: "" }, encoding: "utf8" });
		assert.equal(run.status, status, run.stderr || run.stdout);
	}
});

test("new required jobs fail closed until successful", () => {
	const needs = results(null);
	needs.future_check = { result: "failure" };
	assert.notDeepEqual(checkRequired(needs), []);
	needs.future_check.result = "skipped";
	assert.notDeepEqual(checkRequired(needs), []);
	needs.future_check.result = "success";
	assert.deepEqual(checkRequired(needs), []);
});
