// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import path from "node:path";

export function resolveElectronUserData(options: {
 env: Record<string, string | undefined>;
 homeDir: string;
 desktopDataDir: string;
 isPackaged: boolean;
}): string {
 if (options.isPackaged && options.env.AO_DATA_DIR?.trim()) return path.join(options.desktopDataDir, "orchestrator-board", "electron");
 return options.isPackaged
  ? path.join(options.homeDir, ".ao", "orchestrator-board", "electron")
  : (options.env.AO_DEV_ELECTRON_DIR ?? path.join(options.homeDir, ".ao", "dev", "orchestrator-board", "electron"));
}
