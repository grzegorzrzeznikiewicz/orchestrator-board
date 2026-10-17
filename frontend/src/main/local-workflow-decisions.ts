import { randomBytes } from "node:crypto";
import type { IpcMain, MessageBoxOptions, WebContents } from "electron";
import type { BrowserRuntimeCommand } from "./browser-runtime-link";
import {
	LOCAL_WORKFLOW_CONFIRM_ACTION,
	LOCAL_WORKFLOW_DECIDE_CHANNEL,
	type LocalWorkflowDecisionInput,
	type LocalWorkflowDecisionReply,
} from "../shared/local-workflow-decisions";

type DecisionBody = LocalWorkflowDecisionInput & { intentId: string };
export type DecisionHostOptions = {
	isTrustedOwner(owner: WebContents): boolean;
	showDialog(
		owner: WebContents,
		options: MessageBoxOptions,
	): Promise<{ response: number }>;
	requestDecision(input: DecisionBody, signal: AbortSignal): Promise<void>;
	now?: () => number;
};
export type LocalWorkflowDecisionHost = {
	request(
		owner: WebContents,
		input: LocalWorkflowDecisionInput,
	): Promise<LocalWorkflowDecisionReply>;
	confirm(
		command: BrowserRuntimeCommand,
		signal: AbortSignal,
	): Promise<{ confirmed: true }>;
	dispose(): void;
};
type Pending = {
	owner: WebContents;
	frame: WebContents["mainFrame"];
	input: LocalWorkflowDecisionInput;
	intentId: string;
	expires: number;
	controller: AbortController;
	confirming: boolean;
	result: Promise<LocalWorkflowDecisionReply>;
};
const fields = [
	"sessionId",
	"requestId",
	"workflowId",
	"expectedVersion",
	"kind",
	"comment",
	"snapshotId",
	"testOperationId",
	"reviewRunId",
] as const;
const failure = (code: string, message: string) =>
	Object.assign(new Error(message), { code });
const denied = () =>
	failure(
		"LOCAL_WORKFLOW_DECISION_DENIED",
		"Desktop confirmation was cancelled or is no longer valid.",
	);

function validate(value: unknown): asserts value is LocalWorkflowDecisionInput {
	if (!value || typeof value !== "object" || Array.isArray(value))
		throw failure("LOCAL_WORKFLOW_INVALID", "Invalid decision request.");
	const input = value as Record<string, unknown>;
	const valid =
		Object.keys(input).length === fields.length &&
		fields.every((key) => key in input) &&
		Object.keys(input).every((key) =>
			fields.includes(key as (typeof fields)[number]),
		);
	if (
		!valid ||
		!Number.isSafeInteger(input.expectedVersion) ||
		(input.expectedVersion as number) < 1 ||
		!["accepted", "changes_requested"].includes(input.kind as string)
	)
		throw failure("LOCAL_WORKFLOW_INVALID", "Invalid decision request.");
	for (const key of [
		"sessionId",
		"requestId",
		"workflowId",
		"snapshotId",
		"testOperationId",
		"reviewRunId",
	] as const) {
		const item = input[key];
		if (
			typeof item !== "string" ||
			!item.trim() ||
			item.length > 256 ||
			item.includes("\0") ||
			item.includes("\r") ||
			item.includes("\n")
		)
			throw failure("LOCAL_WORKFLOW_INVALID", "Invalid decision identity.");
	}
	if (
		typeof input.comment !== "string" ||
		[...input.comment].length > 4000 ||
		input.comment.includes("\0") ||
		/[\uD800-\uDFFF]/u.test(input.comment) ||
		(input.kind === "changes_requested" && !input.comment.trim())
	)
		throw failure(
			"LOCAL_WORKFLOW_INVALID",
			"Provide a valid comment of at most 4000 characters; feedback is required when requesting changes.",
		);
}

/** Private intents bridge the local HTTP request and the authenticated broker.
 * HTTP callers cannot mint an intent or synthesize the native response. */
export function createLocalWorkflowDecisionHost(
	options: DecisionHostOptions,
): LocalWorkflowDecisionHost {
	const now = options.now ?? (() => performance.now());
	const intents = new Map<string, Pending>();
	const requests = new Map<string, Pending>();
	let disposed = false;
	const validOwner = (pending: Pending) =>
		!disposed &&
		!pending.owner.isDestroyed() &&
		pending.owner.mainFrame === pending.frame &&
		options.isTrustedOwner(pending.owner);
	const host: LocalWorkflowDecisionHost = {
		async request(owner, value) {
			validate(value);
			const input = { ...value };
			if (disposed || owner.isDestroyed() || !options.isTrustedOwner(owner))
				throw denied();
			const key = JSON.stringify([input.sessionId, input.requestId]);
			const old = requests.get(key);
			if (old) {
				if (
					old.owner !== owner ||
					fields.some((field) => old.input[field] !== input[field])
				)
					throw failure(
						"LOCAL_WORKFLOW_CONFLICT",
						"This decision request already has different inputs.",
					);
				return old.result;
			}
			const controller = new AbortController();
			const pending: Pending = {
				owner,
				frame: owner.mainFrame,
				input,
				intentId: randomBytes(32).toString("base64url"),
				expires: now() + 60_000,
				controller,
				confirming: false,
				result: Promise.resolve({ status: "cancelled" }),
			};
			const cancel = () => controller.abort(denied());
			owner.once("destroyed", cancel);
			owner.once("did-start-navigation", cancel);
			const timer = setTimeout(cancel, 60_000);
			intents.set(pending.intentId, pending);
			requests.set(key, pending);
			pending.result = (async () => {
				let onAbort: () => void = () => {};
				try {
					const aborted = new Promise<never>((_resolve, reject) => {
						onAbort = () => reject(denied());
						controller.signal.addEventListener("abort", onAbort, {
							once: true,
						});
					});
					await Promise.race([
						options.requestDecision(
							{ ...input, intentId: pending.intentId },
							controller.signal,
						),
						aborted,
					]);
					if (
						controller.signal.aborted ||
						!validOwner(pending) ||
						now() >= pending.expires
					)
						throw denied();
					return { status: "recorded" } as const;
				} catch (error) {
					if (
						controller.signal.aborted ||
						(error as { code?: string })?.code ===
							"LOCAL_WORKFLOW_DECISION_DENIED"
					)
						return { status: "cancelled" } as const;
					throw error;
				} finally {
					controller.signal.removeEventListener("abort", onAbort);
					clearTimeout(timer);
					owner.removeListener("destroyed", cancel);
					owner.removeListener("did-start-navigation", cancel);
					intents.delete(pending.intentId);
					requests.delete(key);
				}
			})();
			return pending.result;
		},
		async confirm(command, signal) {
			const args = command.args;
			const pending =
				args && typeof args.intentId === "string"
					? intents.get(args.intentId)
					: undefined;
			if (
				!pending ||
				command.action !== LOCAL_WORKFLOW_CONFIRM_ACTION ||
				pending.confirming ||
				signal.aborted ||
				pending.controller.signal.aborted ||
				!validOwner(pending) ||
				now() >= pending.expires ||
				command.sessionId !== pending.input.sessionId ||
				fields.some((key) => args![key] !== pending.input[key]) ||
				!Number.isSafeInteger(args!.cycle) ||
				(args!.cycle as number) < 1
			)
				throw denied();
			pending.confirming = true;
			const combined = AbortSignal.any([signal, pending.controller.signal]);
			const result = await options.showDialog(pending.owner, {
				type: "question",
				title: "Local workflow",
				message:
					pending.input.kind === "accepted"
						? "Accept these verified changes?"
						: "Send these changes back for corrections?",
				detail: `Session: ${pending.input.sessionId}\nCycle: ${args!.cycle}\nSnapshot: ${pending.input.snapshotId}\n\n${pending.input.comment}`,
				buttons: [
					"Cancel",
					pending.input.kind === "accepted" ? "Accept" : "Request changes",
				],
				defaultId: 0,
				cancelId: 0,
				noLink: true,
				signal: combined,
			});
			if (
				combined.aborted ||
				result.response !== 1 ||
				!validOwner(pending) ||
				now() >= pending.expires
			)
				throw denied();
			return { confirmed: true };
		},
		dispose() {
			disposed = true;
			for (const pending of intents.values())
				pending.controller.abort(denied());
		},
	};
	return host;
}

export function registerLocalWorkflowDecisionIpc(
	ipc: Pick<IpcMain, "handle" | "removeHandler">,
	host: LocalWorkflowDecisionHost,
	isTrustedOwner: (owner: WebContents) => boolean,
): () => void {
	ipc.handle(
		LOCAL_WORKFLOW_DECIDE_CHANNEL,
		(event, input: LocalWorkflowDecisionInput) => {
			if (
				!isTrustedOwner(event.sender) ||
				!event.senderFrame ||
				event.senderFrame !== event.sender.mainFrame
			)
				throw denied();
			return host.request(event.sender, input);
		},
	);
	return () => ipc.removeHandler(LOCAL_WORKFLOW_DECIDE_CHANNEL);
}
