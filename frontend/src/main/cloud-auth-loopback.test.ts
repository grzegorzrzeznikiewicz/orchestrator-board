// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { mkdtemp, rm } from "node:fs/promises";
import { EventEmitter } from "node:events";
import os from "node:os";
import path from "node:path";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({ authorize: vi.fn(), open: vi.fn(), occupied: false, listenPort: 0, listenHost: "" }));
vi.hoisted(() => { delete process.env.AO_CLOUD_AUTH_REDIRECT; });
vi.mock("node:http", async (importOriginal) => {
 const actual = await importOriginal<typeof import("node:http")>();
 const createServer = () => {
 const server = new EventEmitter() as EventEmitter & { close: () => void; listen: (port: number, host: string) => void };
 server.close = vi.fn();
 server.listen = (port, host) => { mocks.listenPort = port; mocks.listenHost = host; queueMicrotask(() => {
  if (mocks.occupied) server.emit("error", Object.assign(new Error("address in use"), { code: "EADDRINUSE" }));
  else server.emit("listening");
 }); };
 return server;
 };
 return { ...actual, createServer, default: { ...actual, createServer } };
});
vi.mock("@workos-inc/node", () => ({ createWorkOS: () => ({ userManagement: { getAuthorizationUrlWithPKCE: mocks.authorize } }) }));
vi.mock("electron", () => ({ app: { isPackaged: true }, shell: { openExternal: mocks.open }, dialog: { showMessageBox: vi.fn() }, ipcMain: { handle: vi.fn() }, safeStorage: { isEncryptionAvailable: () => true, getSelectedStorageBackend: () => "keychain", encryptString: (v: string) => Buffer.from(v), decryptString: (v: Buffer) => v.toString() } }));
vi.mock("./provider-auth-flow", () => ({ providerAuthFlow: vi.fn() }));
import { beginCloudSignIn } from "./cloud-auth";
let directory: string;
beforeEach(async () => { vi.clearAllMocks(); mocks.occupied = false; directory = await mkdtemp(path.join(os.tmpdir(), "ob-auth-loopback-")); mocks.authorize.mockResolvedValue({ url: "https://workos.example/authorize", state: "state", codeVerifier: "verifier" }); });
afterEach(async () => { await rm(directory, { recursive: true, force: true }); });
it("uses the existing loopback callback for packaged fork builds", async () => {
 await beginCloudSignIn(directory);
 expect(mocks.authorize).toHaveBeenCalledWith(expect.objectContaining({ redirectUri: "http://127.0.0.1:3000/callback" }));
 expect(mocks.listenHost).toBe("127.0.0.1"); expect(mocks.listenPort).toBe(3000);
 expect(mocks.open).toHaveBeenCalledWith("https://workos.example/authorize");
});
it("reports a callback port collision before opening the browser", async () => {
 mocks.occupied = true;
 await expect(beginCloudSignIn(directory)).rejects.toThrow("callback");
 expect(mocks.open).not.toHaveBeenCalled();
});
