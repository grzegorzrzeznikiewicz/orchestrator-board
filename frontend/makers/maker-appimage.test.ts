// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { beforeEach, describe, expect, it, vi } from "vitest";

const buildForge = vi.fn<(forge: { dir: string }, options: any) => Promise<string[]>>(
	async () => ["/out/make/Orchestrator-Board.AppImage"],
);
vi.mock("app-builder-lib", () => ({ buildForge }));

import MakerAppImage from "./maker-appimage";

const makeOptions = {
	dir: "/tmp/app/Orchestrator Board-linux-x64",
	makeDir: "/tmp/app/make",
	appName: "Orchestrator Board",
	targetPlatform: "linux" as const,
	targetArch: "x64" as const,
	forgeConfig: {} as never,
	packageJSON: {},
};

beforeEach(() => {
	buildForge.mockClear();
});

describe("MakerAppImage", () => {
	it("targets Linux and is supported for cross-builds", () => {
		const maker = new MakerAppImage();
		expect(maker.name).toBe("appimage");
		expect(maker.defaultPlatforms).toEqual(["linux"]);
		expect(maker.isSupportedOnCurrentPlatform()).toBe(true);
	});

	it("writes callback protocols into the AppImage desktop entry", async () => {
		const protocols = [
			{
				name: "Orchestrator Board authentication callback",
				schemes: ["orchestrator-board"],
			},
		];
		const maker = new MakerAppImage(
			{ appId: "com.gamasoftware.orchestrator-board", protocols },
			["linux"],
		);
		await maker.prepareConfig(makeOptions.targetArch);
		await maker.make(makeOptions);

		const [, options] = buildForge.mock.calls[0];
		expect(options.linux).toEqual(["appImage:x64"]);
		expect(options.config.protocols).toEqual(protocols);
		expect(options.config.publish).toBeNull();
	});

	// Pinned exactly rather than asserted "not legacy": app-builder-lib treats null
	// the same as "0.0.0", so a looser check would stay green on a regression. See
	// the rationale in maker-appimage.ts.
	it("pins the appimage toolset so the artifact does not need libfuse2 (#4006)", async () => {
		const maker = new MakerAppImage({ appId: "com.gamasoftware.orchestrator-board" }, ["linux"]);
		await maker.prepareConfig(makeOptions.targetArch);
		await maker.make(makeOptions);

		const [, options] = buildForge.mock.calls[0];
		expect(options.config.toolsets?.appimage).toBe("1.0.3");
	});
});

it("targets the packaged fork executable in the Linux desktop launcher", async () => {
	const maker = new MakerAppImage({appId: "com.gamasoftware.orchestrator-board", executableName: "orchestrator-board"}, ["linux"]);
	await maker.prepareConfig(makeOptions.targetArch);
	await maker.make(makeOptions);
	expect(buildForge.mock.calls[0][1].config.linux.executableName).toBe("orchestrator-board");
});
