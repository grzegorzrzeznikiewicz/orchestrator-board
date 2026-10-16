import assert from "node:assert/strict";
import { test } from "node:test";
import { spawnSync } from "node:child_process";
import { ESLint } from "eslint";
import { fileURLToPath } from "node:url";

const eslint = new ESLint({ cwd: fileURLToPath(new URL("../", import.meta.url)) });
const lint = async (code, filePath = "src/renderer/lint-fixture.tsx") =>
	(await eslint.lintText(code, { filePath }))[0].messages;

test("accepts typed React code", async () => {
	assert.deepEqual(await lint('import { useState } from "react"; export function Example() { const [value] = useState(1); return <div>{value}</div>; }'), []);
});

test("rejects conditional hooks in new code", async () => {
	const messages = await lint('import { useState } from "react"; export function Example({ enabled }: { enabled: boolean }) { if (enabled) useState(0); return null; }');
	assert.ok(messages.some((message) => message.ruleId === "react-hooks/rules-of-hooks" && message.severity === 2));
});

test("rejects missing effect dependencies", async () => {
	const messages = await lint('import { useEffect } from "react"; export function Example({ value }: { value: string }) { useEffect(() => { console.log(value); }, []); return null; }');
	assert.ok(messages.some((message) => message.ruleId === "react-hooks/exhaustive-deps" && message.severity === 2));
});

test("rejects duplicate enum values", async () => {
	const messages = await lint('export enum Example { First = 1, Second = 1 }', "src/shared/lint-fixture.ts");
	assert.ok(messages.some((message) => message.ruleId === "@typescript-eslint/no-duplicate-enum-values"));
});

test("ignores generated API types", async () => {
	assert.equal(await eslint.isPathIgnored("src/api/schema.ts"), true);
});

test("the CLI suppression baseline does not hide a new file's hook violation", () => {
	const run = spawnSync(process.execPath, [
		fileURLToPath(new URL("../node_modules/eslint/bin/eslint.js", import.meta.url)),
		"--stdin", "--stdin-filename", "src/renderer/ci-new-hook.tsx", "--format", "json",
	], {
		cwd: fileURLToPath(new URL("../", import.meta.url)),
		input: 'import { useState } from "react"; export function Example({ enabled }: { enabled: boolean }) { if (enabled) useState(0); return null; }',
		encoding: "utf8",
	});
	assert.equal(run.status, 1, run.stderr);
	assert.ok(JSON.parse(run.stdout)[0].messages.some((message) => message.ruleId === "react-hooks/rules-of-hooks"));
});
