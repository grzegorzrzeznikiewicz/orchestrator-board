// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import type { OutgoingHttpHeaders } from "node:http";
import { GenericProvider } from "electron-updater/out/providers/GenericProvider";
import { GitHubProvider } from "electron-updater/out/providers/GitHubProvider";
import type { AppUpdater } from "electron-updater/out/AppUpdater";
import type { ProviderRuntimeOptions } from "electron-updater/out/providers/Provider";
import type { UpdateInfo, WindowsUpdateInfo, GithubOptions, GenericServerOptions } from "builder-util-runtime";
import { assertForkArtifactReference, assertForkReleaseUrl, RELEASE_REPO, UpdateSourceError } from "./release-source";

function validateArtifacts(info: WindowsUpdateInfo): void {
  for (const file of info.files ?? []) assertForkArtifactReference(file.url);
  if (info.path) assertForkArtifactReference(info.path);
  for (const file of Object.values(info.packages ?? {})) assertForkArtifactReference(file.path);
}

/** Keep checksum/native-signature verification in the stock updater. */
export class ForkGenericProvider extends GenericProvider {
  constructor(options: GenericServerOptions, updater: AppUpdater, runtime: ProviderRuntimeOptions) {
    super(options, updater, { ...runtime, isUseMultipleRangeRequest: false });
  }
  protected override createRequestOptions(url: URL, headers?: OutgoingHttpHeaders | null) {
    // GenericProvider only fetches the selected manifest. Reject an escaped
    // base URL before the dependency can open any request.
    assertForkReleaseUrl(url.href.split("?")[0]);
    return super.createRequestOptions(url, headers);
  }
  override async getLatestVersion(): Promise<UpdateInfo> {
    const info = await super.getLatestVersion();
    validateArtifacts(info);
    return info;
  }
  override resolveFiles(info: UpdateInfo) {
    validateArtifacts(info);
    const files = super.resolveFiles(info);
    for (const file of files) {
      assertForkReleaseUrl(file.url.href);
      if (file.packageInfo) assertForkReleaseUrl(file.packageInfo.path);
    }
    return files;
  }
}

/** Only stable uses GitHubProvider; prerelease always uses the exact generic feed. */
export class ForkStableProvider extends GitHubProvider {
  constructor(options: GithubOptions, private readonly boundaryUpdater: AppUpdater, runtime: ProviderRuntimeOptions) {
    super(options, boundaryUpdater, runtime);
  }
  protected override createRequestOptions(url: URL, headers?: OutgoingHttpHeaders | null) {
    if (url.href !== `https://github.com/${RELEASE_REPO}/releases.atom`) assertForkReleaseUrl(url.href);
    return super.createRequestOptions(url, headers);
  }
  override async getLatestVersion() {
    if (this.boundaryUpdater.allowPrerelease || this.boundaryUpdater.channel !== "latest" ||
        `${this.options.owner}/${this.options.repo}` !== RELEASE_REPO || this.options.host) {
      throw new UpdateSourceError("stable discovery requires this fork's stable channel.");
    }
    const info = await super.getLatestVersion();
    validateArtifacts(info);
    return info;
  }
  override resolveFiles(info: Awaited<ReturnType<GitHubProvider["getLatestVersion"]>>) {
    validateArtifacts(info);
    const files = super.resolveFiles(info);
    for (const file of files) {
      assertForkReleaseUrl(file.url.href);
      if (file.packageInfo) assertForkReleaseUrl(file.packageInfo.path);
    }
    return files;
  }
}
