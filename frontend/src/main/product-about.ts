// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import path from "node:path";
import { app, dialog, shell, type BaseWindow, type MessageBoxOptions } from "electron";
import { PRODUCT } from "../shared/product";

const licenseFiles = ["LICENSE", "NOTICE", "THIRD-PARTY-LICENSES.txt", "ASSET-NOTICES.txt", "Electron-LICENSE.txt", "Chromium-LICENSES.html"] as const;

export async function showProductAbout(window: BaseWindow | null): Promise<void> {
 const show = (options: MessageBoxOptions) => window ? dialog.showMessageBox(window, options) : dialog.showMessageBox(options);
 const result = await show({
  type: "info",
  title: `About ${PRODUCT.productName}`,
  message: PRODUCT.productName,
  detail: `Version ${app.getVersion()}\nGama Software fork of Agent Orchestrator.\nUpstream: Copyright 2026 Untrivial. Apache-2.0.\n${PRODUCT.sourceUrl}\nFull license and attribution texts are included with this app.`,
  buttons: ["OK", "Licenses"],
  defaultId: 0,
 });
 if (result.response !== 1) return;
 const selected = await show({
  type: "info",
  title: "Licenses and attribution",
  message: "Open the complete bundled text",
  detail: "Third-party dependencies and artwork retain their respective licenses and notices.",
  buttons: [...licenseFiles, "Cancel"],
  cancelId: licenseFiles.length,
 });
 const file = licenseFiles[selected.response];
 if (!file) return;
 const directory = app.isPackaged ? path.join(process.resourcesPath, "licenses") : path.join(__dirname, "../../resources/licenses");
 const error = await shell.openPath(path.join(directory, file));
 if (error) await show({ type: "error", message: "Could not open license text", detail: error });
}
