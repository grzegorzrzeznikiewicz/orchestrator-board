// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { expect, test, type Locator, type Page } from "@playwright/test";

// The current titlebar contains both the wordmark and navigation controls.
// Check their visible boxes, rather than the enclosing titlebar itself, and
// keep the fork name readable on each route.
//
// macOS-only: TitlebarNav (and the bug) gate on navigator.userAgent looking like
// a Mac, read once at module load. Force a Mac UA so this is deterministic
// regardless of the host/CI OS.
test.use({
	userAgent:
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
});

const brand = (page: Page) => page.locator('[data-slot="titlebar-nav"] [data-brand-label]');

// Two boxes overlap iff they intersect on both axes.
function overlaps(a: { x: number; y: number; width: number; height: number }, b: typeof a) {
	return a.x < b.x + b.width && a.x + a.width > b.x && a.y < b.y + b.height && a.y + a.height > b.y;
}

// The brand <span> has `truncate` (overflow:hidden), so it stays "visible" even
// when clipped to nothing. Compare scroll vs client width to prove the wordmark
// is actually fully rendered, not just present-but-clipped.
async function isTruncated(span: Locator) {
	return span.evaluate((el) => el.scrollWidth > el.clientWidth + 1);
}

async function expectBrandClearsCluster(page: Page) {
	const cluster = page.locator('[data-slot="titlebar-nav"]');
	await expect(cluster).toBeVisible();
	const span = brand(page);
	await expect(span).toBeVisible();
	await expect(span).toHaveText("Orchestrator Board");
	await expect.poll(async () => {
		const brandBox = await span.boundingBox();
		if (!brandBox || await isTruncated(span)) return false;
		const controls = await cluster.locator("button").evaluateAll((buttons) => buttons
			.map((button) => button.getBoundingClientRect())
			.filter((box) => box.width > 0 && box.height > 0)
			.map(({ x, y, width, height }) => ({ x, y, width, height })));
		return controls.length > 0 && controls.every((box) => !overlaps(brandBox, box));
	}).toBe(true);
}

test("@P0 home board route: brand clears the macOS titlebar cluster and stays readable", async ({ page }) => {
	await page.goto("/");
	await expect(page.getByText("Projects", { exact: true })).toBeVisible();
	await expectBrandClearsCluster(page);
});

test("@P0 project board route: brand clears the macOS titlebar cluster and stays readable", async ({ page }) => {
	await page.goto("/");
	await expect(page.getByText("Projects", { exact: true })).toBeVisible();

	// In-app nav to /projects/:id (a hard load boots the router at the board).
	await page.locator('[data-sidebar="menu-button"]').filter({ hasText: "ao-demo" }).first().click();
	await expect(page).toHaveURL(/projects\/ao-demo$/);
	await expect(page.getByTestId("board")).toBeVisible();

	await expectBrandClearsCluster(page);
});

test("@P0 brand stays put and readable when navigating board → session", async ({ page }) => {
	await page.goto("/");
	await expect(page.getByText("Projects", { exact: true })).toBeVisible();

	const boardBrandBox = await brand(page).boundingBox();
	expect(boardBrandBox).not.toBeNull();

	await page.locator('[data-sidebar="menu-button"]').filter({ hasText: "ao-demo" }).first().click();
	await expect(page.getByTestId("board")).toBeVisible();
	await page.getByTestId("board-session-card").first().click();
	await expect(page.getByTestId("session-detail")).toBeVisible();

	const sessionBrandBox = await brand(page).boundingBox();
	expect(sessionBrandBox).not.toBeNull();
	// Persistent shell element: no vertical/horizontal jump across the transition.
	expect(Math.abs(sessionBrandBox!.x - boardBrandBox!.x)).toBeLessThanOrEqual(1);
	expect(Math.abs(sessionBrandBox!.y - boardBrandBox!.y)).toBeLessThanOrEqual(1);
	await expectBrandClearsCluster(page);
});
