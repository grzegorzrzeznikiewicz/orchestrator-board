import { EventEmitter } from "node:events";
import type { WebContents } from "electron";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
	createLocalWorkflowDecisionHost,
	registerLocalWorkflowDecisionIpc,
	type LocalWorkflowDecisionHost,
} from "./local-workflow-decisions";
import type { LocalWorkflowDecisionInput } from "../shared/local-workflow-decisions";

const input: LocalWorkflowDecisionInput = {
	sessionId: "session",
	requestId: "request",
	workflowId: "flow",
	expectedVersion: 5,
	kind: "accepted",
	comment: "checked",
	snapshotId: "snapshot",
	testOperationId: "tests",
	reviewRunId: "review",
};
const hosts: LocalWorkflowDecisionHost[] = [];
afterEach(() => {
	hosts.splice(0).forEach((h) => h.dispose());
	vi.useRealTimers();
});

function setup(response = 1) {
	let now = 0;
	let trusted = true;
	const owner = Object.assign(new EventEmitter(), {
		id: 1,
		isDestroyed: () => false,
		mainFrame: {},
	}) as unknown as WebContents;
	const dialog = vi.fn(async () => ({ response }));
	let beforeConfirm: (
		body: LocalWorkflowDecisionInput & { intentId: string },
	) => void = () => {};
	let finishDialog: (() => void) | undefined;
	const requestDecision = vi.fn(
		async (
			body: LocalWorkflowDecisionInput & { intentId: string },
			signal: AbortSignal,
		) => {
			beforeConfirm(body);
			await host.confirm(
				{
					type: "command",
					requestId: "broker",
					sessionId: body.sessionId,
					action: "__confirm-local-workflow-decision",
					args: { ...body, cycle: 1 },
				},
				signal,
			);
		},
	);
	const host = createLocalWorkflowDecisionHost({
		isTrustedOwner: (o) => trusted && o === owner,
		showDialog: dialog,
		requestDecision,
		now: () => now,
	});
	hosts.push(host);
	return {
		host,
		owner,
		dialog,
		requestDecision,
		setTime: (n: number) => {
			now = n;
		},
		setTrusted: (v: boolean) => {
			trusted = v;
		},
		before: (f: typeof beforeConfirm) => {
			beforeConfirm = f;
		},
		holdDialog: () => {
			dialog.mockImplementation(
				() =>
					new Promise((resolve) => {
						finishDialog = () => resolve({ response: 1 });
					}),
			);
			return () => finishDialog?.();
		},
	};
}

describe("local workflow desktop decisions", () => {
	it("retries a committed decision after response loss without another native dialog", async () => {
		const s = setup();
		let recordedRequest: string | undefined;
		const privateIntents: string[] = [];
		s.requestDecision.mockImplementation(async (body, signal) => {
			privateIntents.push(body.intentId);
			if (recordedRequest === body.requestId) return;
			await s.host.confirm({
				type: "command", requestId: "broker", sessionId: body.sessionId,
				action: "__confirm-local-workflow-decision", args: { ...body, cycle: 1 },
			}, signal);
			recordedRequest = body.requestId;
			throw new Error("Response lost after durable decision");
		});
		await expect(s.host.request(s.owner, input)).rejects.toThrow("Response lost");
		await expect(s.host.request(s.owner, { ...input })).resolves.toEqual({ status: "recorded" });
		expect(recordedRequest).toBe(input.requestId);
		expect(s.requestDecision).toHaveBeenCalledTimes(2);
		expect(s.requestDecision.mock.calls.map(([body]) => body.requestId)).toEqual([
			input.requestId, input.requestId,
		]);
		expect(privateIntents).toHaveLength(2);
		expect(new Set(privateIntents).size).toBe(2);
		expect(s.dialog).toHaveBeenCalledTimes(1);
	});
	it("confirms only an exact private intent and explicit native response", async () => {
		const s = setup();
		await expect(
			s.host.confirm(
				{
					type: "command",
					requestId: "unknown",
					sessionId: "session",
					action: "__confirm-local-workflow-decision",
					args: { ...input, intentId: "forged", cycle: 1 },
				},
				new AbortController().signal,
			),
		).rejects.toMatchObject({ code: "LOCAL_WORKFLOW_DECISION_DENIED" });
		expect(s.dialog).not.toHaveBeenCalled();
		s.before((body) => {
			expect(body.intentId).toMatch(/^[\w-]{43}$/);
			expect(body.intentId).not.toBe(input.requestId);
		});
		await expect(s.host.request(s.owner, input)).resolves.toEqual({
			status: "recorded",
		});
		expect(s.dialog).toHaveBeenCalledWith(
			s.owner,
			expect.objectContaining({
				defaultId: 0,
				cancelId: 0,
				detail: expect.stringContaining("snapshot"),
				signal: expect.any(AbortSignal),
			}),
		);
	});
	it("never records cancellation and rejects modified challenge inputs", async () => {
		const s = setup(0);
		await expect(s.host.request(s.owner, input)).resolves.toEqual({
			status: "cancelled",
		});
		const changed = setup();
		changed.before((body) => {
			body.comment = "forged comment";
		});
		await expect(changed.host.request(changed.owner, input)).resolves.toEqual({
			status: "cancelled",
		});
		expect(changed.dialog).not.toHaveBeenCalled();
	});
	it("expires at sixty seconds and refuses a late native approval", async () => {
		const s = setup();
		s.before(() => s.setTime(60_000));
		await expect(s.host.request(s.owner, input)).resolves.toEqual({
			status: "cancelled",
		});
		expect(s.dialog).not.toHaveBeenCalled();
		const late = setup();
		const finish = late.holdDialog();
		const pending = late.host.request(late.owner, input);
		await vi.waitFor(() => expect(late.dialog).toHaveBeenCalled());
		late.setTime(60_000);
		finish();
		await expect(pending).resolves.toEqual({ status: "cancelled" });
	});
	it.each(["destroyed", "did-start-navigation", "connection"])(
		"refuses owner loss or abort: %s",
		async (event) => {
			const s = setup();
			const finish = s.holdDialog();
			const pending = s.host.request(s.owner, input);
			await vi.waitFor(() => expect(s.dialog).toHaveBeenCalled());
			if (event === "connection") s.host.dispose();
			else (s.owner as unknown as EventEmitter).emit(event);
			finish();
			await expect(pending).resolves.toEqual({ status: "cancelled" });
		},
	);
	it("deduplicates in-flight requests and rejects changed retries", async () => {
		const s = setup();
		const finish = s.holdDialog();
		const first = s.host.request(s.owner, input);
		const second = s.host.request(s.owner, { ...input });
		await expect(
			s.host.request(s.owner, { ...input, comment: "changed" }),
		).rejects.toMatchObject({ code: "LOCAL_WORKFLOW_CONFLICT" });
		await vi.waitFor(() => expect(s.dialog).toHaveBeenCalledTimes(1));
		finish();
		await expect(Promise.all([first, second])).resolves.toEqual([
			{ status: "recorded" },
			{ status: "recorded" },
		]);
		expect(s.requestDecision).toHaveBeenCalledTimes(1);
	});
	it("validates Unicode comment length, required feedback and unknown fields", async () => {
		const s = setup();
		await expect(
			s.host.request(s.owner, { ...input, comment: "😀".repeat(4000) }),
		).resolves.toEqual({ status: "recorded" });
		for (const value of [
			{ ...input, comment: "😀".repeat(4001) },
			{ ...input, comment: "\u0000" },
			{ ...input, kind: "changes_requested", comment: "  " },
			{ ...input, hostId: "remote" },
			{ ...input, actor: "user" },
		]) {
			await expect(
				s.host.request(s.owner, value as LocalWorkflowDecisionInput),
			).rejects.toMatchObject({ code: "LOCAL_WORKFLOW_INVALID" });
		}
		s.setTrusted(false);
		await expect(s.host.request(s.owner, input)).rejects.toMatchObject({
			code: "LOCAL_WORKFLOW_DECISION_DENIED",
		});
	});
	it("binds IPC to a trusted shell main frame", async () => {
		const s = setup();
		let handler: (event: unknown, input: unknown) => unknown = () => {};
		registerLocalWorkflowDecisionIpc(
			{
				handle: (
					_channel: string,
					fn: (event: unknown, input: unknown) => unknown,
				) => {
					handler = fn as typeof handler;
				},
				removeHandler: vi.fn(),
			} as never,
			s.host,
			(o) => o === s.owner,
		);
		await expect(async () =>
			handler({ sender: s.owner, senderFrame: {} }, input),
		).rejects.toMatchObject({ code: "LOCAL_WORKFLOW_DECISION_DENIED" });
		await expect(async () =>
			handler(
				{
					sender: { mainFrame: s.owner.mainFrame },
					senderFrame: s.owner.mainFrame,
				},
				input,
			),
		).rejects.toMatchObject({ code: "LOCAL_WORKFLOW_DECISION_DENIED" });
		await expect(
			handler({ sender: s.owner, senderFrame: s.owner.mainFrame }, input),
		).resolves.toEqual({ status: "recorded" });
	});
});
