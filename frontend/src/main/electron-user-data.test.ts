// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { expect, it } from "vitest";
import { resolveElectronUserData } from "./electron-user-data";
const base = { homeDir: "/home/user", desktopDataDir: "/task/data", isPackaged: true };
it("isolates the packaged fork profile from the original encryption identity", () => {
 expect(resolveElectronUserData({ ...base, env: {} })).toBe("/home/user/.ao/orchestrator-board/electron");
 expect(resolveElectronUserData({ ...base, env: { AO_DATA_DIR: "   " } })).toBe("/home/user/.ao/orchestrator-board/electron");
 expect(resolveElectronUserData({ ...base, env: { AO_DEV_ELECTRON_DIR: "/dev-only" } })).toBe("/home/user/.ao/orchestrator-board/electron");
});
it("isolates packaged Chromium state under the resolved explicit daemon data directory", () => {
 expect(resolveElectronUserData({ ...base, env: { AO_DATA_DIR: "../data", AO_DEV_ELECTRON_DIR: "/dev-only" } })).toBe("/task/data/orchestrator-board/electron");
});
it("isolates the default developer profile and preserves an explicit developer override", () => {
 expect(resolveElectronUserData({ ...base, isPackaged: false, env: {} })).toBe("/home/user/.ao/dev/orchestrator-board/electron");
 expect(resolveElectronUserData({ ...base, isPackaged: false, env: { AO_DEV_ELECTRON_DIR: "/custom/dev", AO_DATA_DIR: "/task/data" } })).toBe("/custom/dev");
});
