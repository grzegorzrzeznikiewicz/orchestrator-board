// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

// @vitest-environment node
import { afterEach, describe, expect, it, vi } from "vitest";
import { createServer, request } from "node:http";
import { createHash } from "node:crypto";
import { createRequire } from "node:module";
import { mkdtempSync, writeFileSync, rmSync, existsSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { ForkGenericProvider } from "../src/main/release-source-provider";
import { RELEASE_REPO } from "../src/main/release-source";

const require = createRequire(import.meta.url);
const { AppUpdater } = require("electron-updater/out/AppUpdater.js");
const { MacUpdater } = require("electron-updater/out/MacUpdater.js");
const { ElectronHttpExecutor } = require("electron-updater/out/electronHttpExecutor.js");
const { HttpExecutor } = require("builder-util-runtime");
const dirs = [];
afterEach(() => { vi.restoreAllMocks(); for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true }); });

// Only transport is replaced. Keep the original requested HTTPS URL in evidence;
// the pinned provider, check, download, SHA-512 and promotion paths are real.
class LoopbackExecutor extends HttpExecutor {
  constructor(port) { super(); this.port = port; this.requested = []; }
  createRequest(options, callback) {
    this.requested.push(`${options.protocol}//${options.hostname}${options.path}`);
    return request({ ...options, protocol: "http:", hostname: "127.0.0.1", port: this.port, agent: false }, callback);
  }
  download(...args) { return ElectronHttpExecutor.prototype.download.apply(this, args); }
}

async function withUpdater(mode, action) {
  const dir = mkdtempSync(join(tmpdir(), "ob-feed-contract-")); dirs.push(dir);
  const configPath = join(dir, "app-update.yml");
  writeFileSync(configPath, "provider: github\nowner: grzegorzrzeznikiewicz\nrepo: orchestrator-board\nupdaterCacheDirName: orchestrator-board-updater\n");
  const bytes = Buffer.from("This payload is deliberately not a signed archive.");
  // A universal fixture reaches the real checksum check on both x64 and arm64.
  const manifest = {
    version: mode === "current" ? "1.0.0-nightly.1" : "2.0.0-nightly.1",
    files: [{ url: mode === "foreign" ? "https://github.com/Untrivial-ai/agent-orchestrator/releases/download/v2.0.0/update.zip" : "orchestrator-board-darwin-universal.zip",
      sha512: createHash("sha512").update("different bytes").digest("base64"), size: bytes.length }],
  };
  const server = createServer((req, res) => {
    if (new URL(req.url, "http://localhost").pathname.endsWith(".yml")) {
      res.writeHead(mode === "missing" ? 404 : 200, { "Content-Type": "text/yaml" });
      res.end(mode === "missing" ? "not published" : JSON.stringify(manifest));
    } else { res.writeHead(200, { "Content-Length": bytes.length }); res.end(bytes); }
  });
  await new Promise(resolve => server.listen(0, "127.0.0.1", resolve));
  // Build the real JS AppUpdater with its supported injected app adapter. Avoid
  // constructing Electron/Squirrel outside Electron. No native success is faked:
  // all cases below must finish before any handoff to MacUpdater.updateDownloaded.
  const updater = new AppUpdater(undefined, {
    version: "1.0.0-nightly.1", name: "Orchestrator Board", isPackaged: true,
    appUpdateConfigPath: configPath, userDataPath: dir, baseCachePath: dir,
    whenReady: async () => {}, onQuit: () => {},
  });
  Object.setPrototypeOf(updater, MacUpdater.prototype);
  updater.logger = { info: vi.fn(), warn: vi.fn(), error: vi.fn(), debug: vi.fn() };
  updater.httpExecutor = new LoopbackExecutor(server.address().port);
  Object.defineProperty(updater, "isAddNoCacheQuery", { value: false });
  // Exercise the macOS update protocol on all CI host platforms.
  const createRuntime = updater.createProviderRuntimeOptions.bind(updater);
  updater.createProviderRuntimeOptions = () => ({ ...createRuntime(), platform: "darwin" });
  updater.channel = "nightly";
  updater.allowPrerelease = true;
  updater.autoDownload = mode === "foreign";
  updater.disableDifferentialDownload = true;
  const nativeHandoff = vi.spyOn(updater, "updateDownloaded");
  const downloaded = vi.fn(); updater.on("update-downloaded", downloaded);
  const current = vi.fn(); updater.on("update-not-available", current);
  updater.setFeedURL({ provider: "custom", updateProvider: ForkGenericProvider,
    url: `https://github.com/${RELEASE_REPO}/releases/download/v2.0.0-nightly.1`, channel: "nightly" });
  try { await action({ updater, requested: updater.httpExecutor.requested, nativeHandoff, downloaded, current, dir }); }
  finally { updater.closeServerIfExists(); await new Promise(resolve => server.close(resolve)); }
}

describe("installed electron-updater 6.8.9 fork feed contract", () => {
  it("uses the dependency version whose channel and integrity behavior we inspected", () => {
    expect(require("electron-updater/package.json").version).toBe("6.8.9");
  });
  it("returns current without downloading a payload", async () => {
    await withUpdater("current", async ({ updater, requested, current, nativeHandoff }) => {
      const result = await updater.checkForUpdates();
      expect(result.isUpdateAvailable).toBe(false);
      expect(current).toHaveBeenCalledTimes(1);
      expect(requested).toEqual([`https://github.com/${RELEASE_REPO}/releases/download/v2.0.0-nightly.1/nightly-mac.yml`]);
      expect(nativeHandoff).not.toHaveBeenCalled();
    });
  });
  it("fails a missing nightly without the dependency's GitHubProvider latest fallback", async () => {
    await withUpdater("missing", async ({ updater, requested, nativeHandoff }) => {
      await expect(updater.checkForUpdates()).rejects.toThrow(/Cannot find channel/);
      expect(requested).toHaveLength(1);
      expect(requested[0]).toContain("/nightly-mac.yml");
      expect(nativeHandoff).not.toHaveBeenCalled();
    });
  });
  it("rejects a foreign artifact before automatic download", async () => {
    await withUpdater("foreign", async ({ updater, requested, nativeHandoff }) => {
      await expect(updater.checkForUpdates()).rejects.toThrow(/outside this fork/);
      expect(requested).toHaveLength(1);
      expect(nativeHandoff).not.toHaveBeenCalled();
    });
  });
  it("rejects real SHA-512 mismatch before cache promotion or native staging", async () => {
    await withUpdater("digest", async ({ updater, requested, nativeHandoff, downloaded, dir }) => {
      const result = await updater.checkForUpdates();
      expect(result.isUpdateAvailable).toBe(true);
      await expect(updater.downloadUpdate()).rejects.toThrow(/sha512 checksum mismatch/i);
      expect(requested).toHaveLength(2);
      expect(requested.every(url => url.startsWith(`https://github.com/${RELEASE_REPO}/releases/download/`))).toBe(true);
      expect(requested.some(url => url.includes(".blockmap"))).toBe(false);
      expect(existsSync(join(dir, "orchestrator-board-updater", "pending", "update-info.json"))).toBe(false);
      expect(downloaded).not.toHaveBeenCalled();
      expect(nativeHandoff).not.toHaveBeenCalled();
    });
  });
});
