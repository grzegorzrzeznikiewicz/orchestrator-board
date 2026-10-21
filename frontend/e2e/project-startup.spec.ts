// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).

import { expect, test } from "@playwright/test";
import { installFakeAgent } from "./support/fake-bridge";
import type { WorkspaceSummary } from "../src/renderer/types/workspace";

// Renderer restart regression with the existing fake daemon/preload boundary.
// The second document starts at the same bare URL as a new Electron window;
// localStorage survives, while the router and in-memory state do not.
test("renderer: reopening starts on the last selected project board @P0", async ({ page }) => {
	await installFakeAgent(page, { workers: [] });
	await page.addInitScript(() => {
		const fake = window.__aoFakeAgent!;
		const snapshot = fake.snapshot;
		fake.snapshot = () => (snapshot() as WorkspaceSummary[]).map((workspace) => ({ ...workspace, reviewMode: "local" }));
	});
	await page.goto("/#/projects/fake-proj");
	await expect(page.getByTestId("board")).toBeVisible();
	await expect.poll(() => page.evaluate(() => JSON.parse(localStorage.getItem("ao.project-history") ?? "{}")["fake-proj"])).toBeTruthy();

	await page.goto("/");
	await expect(page).toHaveURL(/#\/projects\/fake-proj$/);
	await expect(page.getByTestId("board")).toBeVisible();
	await expect(page.getByTestId("board").locator('section[aria-label="Local workflow"] [data-column]')).toHaveCount(6);

	await page.getByRole("button", { name: "Search", exact: true }).click();
	await page.getByRole("option", { name: "Go to home", exact: true }).click();
	await expect(page).toHaveURL(/\/#\/$/);
	await expect(page.getByText("Jump back right in")).toBeVisible();
});
