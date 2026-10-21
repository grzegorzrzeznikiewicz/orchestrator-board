// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { appendFileSync } from "node:fs";
import { pathToFileURL } from "node:url";
import { areas } from "./changes.mjs";

export function checkRequired(needs) {
	const errors = [];
	const outputs = needs?.changes?.outputs ?? {};
	for (const area of areas) {
		if (!["true", "false"].includes(outputs[area])) errors.push(`Missing or invalid changes output: ${area}`);
	}
	const required = {
		changes: true,
		workflow: true,
		secrets: true,
		go: outputs.backend === "true" || outputs.cloud === "true",
		frontend: outputs.frontend === "true",
		cli: outputs.cli === "true",
		mobile: outputs.mobile === "true",
		e2e_gate: outputs.e2e_gate === "true",
	};
	for (const job of new Set([...Object.keys(required), ...Object.keys(needs ?? {})])) {
		const expected = required[job] ?? true;
		const result = needs?.[job]?.result;
		if (result !== "success" && !(result === "skipped" && !expected)) errors.push(`${job}: ${result ?? "missing"}${expected ? " (required)" : ""}`);
	}
	return errors;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
	const needs = JSON.parse(process.env.CI_NEEDS);
	const errors = checkRequired(needs);
	const report = errors.length ? `CI failed:\n${errors.map((error) => `- ${error}`).join("\n")}` : "All required checks passed; only unaffected suites were skipped.";
	console.log(report);
	if (process.env.GITHUB_STEP_SUMMARY) appendFileSync(process.env.GITHUB_STEP_SUMMARY, `### CI / required\n\n${report}\n`);
	if (errors.length) process.exitCode = 1;
}
