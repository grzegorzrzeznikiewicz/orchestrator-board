// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { app, dialog, shell } from "electron";
import semver from "semver";
import product from "../../product.json";
import { readReleaseSource, RELEASE_REPO, RELEASES_URL, assertForkReleaseUrl } from "./release-source";

const FLOOR_URL =
  `https://raw.githubusercontent.com/${RELEASE_REPO}/main/desktop-version-floor.json`;

const FETCH_TIMEOUT_MS = 10_000;

type Floor = {
  min?: string;
  latest?: string;
  downloadUrl?: string;
};

const DEFAULT_DOWNLOAD_URL =
  `${RELEASES_URL}/latest`;

function usableVersion(v: string | null | undefined): string | null {
  const trimmed = v?.trim();
  return trimmed && /^\d{1,4}(\.\d{1,4}){0,3}(-[a-zA-Z0-9.]+)?$/.test(trimmed)
    ? trimmed
    : null;
}

function isBelow(running: string, floor: string): boolean {
  const r = semver.valid(semver.coerce(running));
  const f = semver.valid(semver.coerce(floor));
  if (!r || !f) return false;
  return semver.lt(r, f);
}

export async function checkDesktopVersionFloor(): Promise<void> {
  if (!app.isPackaged) return;
  let floor: Floor;
  try {
    readReleaseSource();
    const response = await fetch(FLOOR_URL, {
      cache: "no-store",
      redirect: "error",
      headers: {
        "User-Agent": `ao-desktop/${app.getVersion()}`,
        Accept: "application/json",
      },
      signal: AbortSignal.timeout(FETCH_TIMEOUT_MS),
    });
    if (!response.ok) return;
    floor = (await response.json()) as Floor;
  } catch (error) {
    console.warn("[desktop-version-floor]", error);
    return;
  }

  const running = app.getVersion();
  const downloadUrl = floor.downloadUrl || DEFAULT_DOWNLOAD_URL;
  try { assertForkReleaseUrl(downloadUrl); } catch (error) {
    console.warn("[desktop-version-floor]", error);
    return;
  }
  const minVersion = usableVersion(floor.min);
  const latestVersion = usableVersion(floor.latest);

  if (minVersion && isBelow(running, minVersion)) {
    const result = await dialog.showMessageBox({
      type: "warning",
      buttons: ["Download Update", "Quit"],
      defaultId: 0,
      cancelId: 1,
      title: "Update Required",
      message: `${product.productName} v${running} is no longer supported.`,
      detail:
        `Version ${minVersion} or later is required. ` +
        "Download the latest release to continue using the app.",
      noLink: true,
    });
    if (result.response === 0) {
      void shell.openExternal(downloadUrl);
    }
    app.quit();
    return;
  }

  if (latestVersion && isBelow(running, latestVersion)) {
    const result = await dialog.showMessageBox({
      type: "info",
      buttons: ["Download Update", "Later"],
      defaultId: 0,
      cancelId: 1,
      title: "Update Available",
      message: `A newer version of ${product.productName} is available.`,
      detail:
        `You are on v${running}. Version ${latestVersion} is recommended. ` +
        "You can update now or continue using the current version.",
      noLink: true,
    });
    if (result.response === 0) {
      void shell.openExternal(downloadUrl);
    }
  }
}
