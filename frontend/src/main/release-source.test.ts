// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

// @vitest-environment node
import { describe, it, expect, vi } from "vitest";
import { MAC_V2_REPOSITORY } from "./mac-differential-v2-protocol";
import semver from "semver";
import { HttpError } from "builder-util-runtime";
import type { AppUpdater } from "electron-updater/out/AppUpdater";
import type { ProviderRuntimeOptions } from "electron-updater/out/providers/Provider";
import { ForkGenericProvider, ForkStableProvider } from "./release-source-provider";
import { assertForkArtifactReference, assertForkReleaseUrl, parseReleaseSource, RELEASE_REPO } from "./release-source";

const validYml = "provider: github\nowner: grzegorzrzeznikiewicz\nrepo: orchestrator-board\nupdaterCacheDirName: orchestrator-board-updater\n";
const release = `https://github.com/${RELEASE_REPO}/releases/download/v2.0.0-nightly.1`;
const updater = { channel: "nightly", allowPrerelease: true, isAddNoCacheQuery: false } as unknown as AppUpdater;
function runtime(request: ReturnType<typeof vi.fn>): ProviderRuntimeOptions {
  return { platform: "darwin", isUseMultipleRangeRequest: false, executor: { request } } as unknown as ProviderRuntimeOptions;
}

describe("bundled release source", () => {
  it("keeps the dormant differential protocol on this fork too", () => { expect(MAC_V2_REPOSITORY).toBe(RELEASE_REPO); });
  it("accepts the exact packaged fork config", () => {
    expect(parseReleaseSource(validYml)).toEqual({ owner: "grzegorzrzeznikiewicz", repo: "orchestrator-board" });
  });
  it.each([
    "", "- provider: github", validYml.replace("grzegorzrzeznikiewicz", "Untrivial-ai"),
    validYml.replace("orchestrator-board-updater", "agent-orchestrator-updater"),
    validYml + "host: evil.example\n", validYml + "owner: other\n", validYml + "private: true\n",
    validYml + "url: https://github.com/another/repo\n", validYml + "channel: alpha\n",
  ])("rejects malformed/foreign configuration %#", text => {
    expect(() => parseReleaseSource(text)).toThrow(/update configuration/);
  });
  it.each([
    "https://github.com/Untrivial-ai/agent-orchestrator/releases/latest",
    "https://github.com/other/repo/releases/latest", "http://github.com/" + RELEASE_REPO + "/releases/latest",
    `https://github.com/${RELEASE_REPO}-other/releases/latest`,
    `https://github.com/${RELEASE_REPO}/releases/../../other/releases/latest`,
    `https://github.com/${RELEASE_REPO}/releases/%2e%2e/other`,
    `https://user:password@github.com/${RELEASE_REPO}/releases/latest`,
  ])("rejects foreign or ambiguous release link %s", url => {
    expect(() => assertForkReleaseUrl(url)).toThrow(/update configuration/);
  });
  it.each(["//evil.example/file.zip", "../../other/file.zip", "/other/repo/file.zip", "file.zip?redirect=other", "file.zip#hash"])("rejects non-asset reference %s", url => {
    expect(() => assertForkArtifactReference(url)).toThrow(/update configuration/);
  });
});

describe("pinned electron-updater provider boundary", () => {
  it("preserves single-range behavior through the custom-provider factory", () => {
    const provider = new ForkGenericProvider({ provider: "generic", url: release }, updater, { ...runtime(vi.fn()), isUseMultipleRangeRequest: true });
    expect(provider.isUseMultipleRangeRequest).toBe(false);
  });
  it("does not request latest when the requested nightly manifest is missing", async () => {
    const request = vi.fn().mockRejectedValue(new HttpError(404, "missing nightly"));
    const provider = new ForkGenericProvider({ provider: "generic", url: release, channel: "nightly" }, updater, runtime(request));
    await expect(provider.getLatestVersion()).rejects.toThrow(/Cannot find channel/);
    expect(request).toHaveBeenCalledTimes(1);
    expect(request.mock.calls[0][0]).toMatchObject({ hostname: "github.com", path: `/${RELEASE_REPO}/releases/download/v2.0.0-nightly.1/nightly-mac.yml` });
  });
  it.each(["files", "path", "packages"])("rejects a foreign %s URL before returning a download offer", async field => {
    const foreign = "https://github.com/Untrivial-ai/agent-orchestrator/releases/download/v2.0.0/update.zip";
    const manifest = { version: "2.0.0", files: [{ url: "update.zip", sha512: "checksum" }],
      ...(field === "files" ? { files: [{ url: foreign, sha512: "checksum" }] } : {}),
      ...(field === "path" ? { path: foreign } : {}),
      ...(field === "packages" ? { packages: { arm64: { path: foreign, sha512: "checksum" } } } : {}),
    };
    const request = vi.fn().mockResolvedValue(JSON.stringify(manifest));
    const provider = new ForkGenericProvider({ provider: "generic", url: release }, updater, runtime(request));
    await expect(provider.getLatestVersion()).rejects.toThrow(/outside this fork/);
    expect(request).toHaveBeenCalledTimes(1);
  });
  it("resolves a valid own-fork asset using the installed provider", async () => {
    const request = vi.fn().mockResolvedValue(JSON.stringify({ version: "2.0.0", files: [{ url: "update.zip", sha512: "checksum" }] }));
    const provider = new ForkGenericProvider({ provider: "generic", url: release }, updater, runtime(request));
    const info = await provider.getLatestVersion();
    expect(provider.resolveFiles(info)[0].url.href).toBe(`${release}/update.zip`);
  });
  it("rejects a traversal release tag before requesting a foreign manifest", async () => {
    const request = vi.fn()
      .mockResolvedValueOnce('<feed><entry><title>release</title><link href="https://github.com/grzegorzrzeznikiewicz/orchestrator-board/releases/tag/v2.0.0"/><content>notes</content></entry></feed>')
      .mockResolvedValueOnce(JSON.stringify({ tag_name: "../../../../Untrivial-ai/agent-orchestrator/releases/download/v2.0.0" }))
      .mockResolvedValueOnce(JSON.stringify({ version: "2.0.0", files: [{ url: "update.zip", sha512: "checksum" }] }));
    const stable = { channel: "latest", allowPrerelease: false, currentVersion: new semver.SemVer("1.0.0") } as AppUpdater;
    const provider = new ForkStableProvider({ provider: "github", owner: "grzegorzrzeznikiewicz", repo: "orchestrator-board" }, stable, runtime(request));
    await expect(provider.getLatestVersion()).rejects.toThrow(/outside this fork/);
    expect(request).toHaveBeenCalledTimes(2);
  });

  it("refuses prerelease use of the stable provider before requests", async () => {
    const request = vi.fn();
    const provider = new ForkStableProvider({ provider: "github", owner: "grzegorzrzeznikiewicz", repo: "orchestrator-board" }, updater, runtime(request));
    await expect(provider.getLatestVersion()).rejects.toThrow(/stable/i);
    expect(request).not.toHaveBeenCalled();
  });
});
