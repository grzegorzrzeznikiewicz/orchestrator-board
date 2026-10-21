// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { defineConfig } from "@playwright/test";

// Overridable because 5173 is not ours alone: another worktree's
// `electron-forge start` also listens there, and with reuseExistingServer the
// suite silently runs against ITS renderer — which is built for Electron and
// fails in confusing, unrelated ways. Set AO_E2E_PORT to run alongside one.
const port = Number(process.env.AO_E2E_PORT ?? 5173);

export default defineConfig({
	testDir: "e2e",
	forbidOnly: !!process.env.CI,
	workers: process.env.CI ? 2 : undefined,
	retries: process.env.CI ? 1 : 0,
	// CI can still be loading the renderer's lazy session modules after goto.
	// Retry the readiness condition instead of failing a healthy slow startup.
	expect: { timeout: process.env.CI ? 15_000 : 5_000 },
	reporter: process.env.CI
		? [["list"], ["html", { open: "never" }], ["junit", { outputFile: "test-results/playwright.xml" }]]
		: "list",
	use: {
		baseURL: `http://127.0.0.1:${port}`,
		trace: "retain-on-failure",
		screenshot: "only-on-failure",
	},
	webServer: {
		// dev:web serves the renderer alone (VITE_NO_ELECTRON=1) — no Electron child to
		// launch, which is all the browser-based e2e suite needs.
		command: `npm run dev:web -- --port ${port} --host 127.0.0.1`,
		port,
		reuseExistingServer: !process.env.CI,
	},
});
