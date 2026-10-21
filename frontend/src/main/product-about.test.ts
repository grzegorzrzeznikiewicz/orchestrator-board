// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { beforeEach, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({ show: vi.fn(), open: vi.fn() }));
vi.mock("electron", () => ({ app: { isPackaged: true, getVersion: () => "1.2.3" }, dialog: { showMessageBox: mocks.show }, shell: { openPath: mocks.open } }));
import { showProductAbout } from "./product-about";
import type { BaseWindow } from "electron";
beforeEach(() => { vi.clearAllMocks(); mocks.open.mockResolvedValue(""); });
it("retains upstream attribution and offers full bundled license texts", async () => {
 mocks.show.mockResolvedValue({ response: 0 });
 await showProductAbout({} as BaseWindow);
 expect(mocks.show).toHaveBeenCalledWith(expect.anything(), expect.objectContaining({
  message: "Orchestrator Board",
  detail: expect.stringContaining("Agent Orchestrator"),
  buttons: ["OK", "Licenses"],
 }));
});
it("opens the selected full license from packaged resources", async () => {
 Object.defineProperty(process, "resourcesPath", { value: "/test/resources", configurable: true });
 mocks.show.mockResolvedValueOnce({ response: 1 }).mockResolvedValueOnce({ response: 2 });
 await showProductAbout({} as BaseWindow);
 expect(mocks.open).toHaveBeenCalledWith("/test/resources/licenses/THIRD-PARTY-LICENSES.txt");
});

it("opens About without a parent after the desktop window closes", async () => {
 mocks.show.mockResolvedValue({ response: 0 });
 await showProductAbout(null);
 expect(mocks.show).toHaveBeenCalledWith(expect.objectContaining({ message: "Orchestrator Board" }));
 expect(mocks.show.mock.calls[0]).toHaveLength(1);
});
