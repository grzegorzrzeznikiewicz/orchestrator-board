import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { LocalWorkflowPanel } from "./LocalWorkflowPanel";

const { get, post, decide, desktop } = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  decide: vi.fn(),
  desktop: { available: true },
}));
vi.mock("../lib/bridge", () => ({
  aoBridge: {
    localWorkflow: {
      get available() {
        return desktop.available;
      },
      decide,
    },
  },
}));
vi.mock("../lib/host-clients", () => ({
  clientForSessionHost: () => ({ GET: get, POST: post }),
}));
const run = {
  id: "wf",
  sessionId: "worker",
  version: 4,
  phase: "blocked",
  cycle: 3,
  maxRounds: 3,
  stopRequested: false,
  testCommands: ["npm test"],
  blockCode: "round_limit",
  blockMessage: "Required findings remain",
  createdAt: "2026-10-16T12:00:00Z",
  updatedAt: "2026-10-16T12:00:00Z",
};
const snapshot = {
  run,
  current: false,
  operations: [],
  tests: [],
  requiredFindings: [
    {
      kind: "required",
      path: "input.ts",
      line: 7,
      reason: "Validate empty input",
    },
  ],
};
function show() {
  return render(
    <QueryClientProvider
      client={
        new QueryClient({
          defaultOptions: {
            queries: { retry: false },
            mutations: { retry: false },
          },
        })
      }
    >
      <LocalWorkflowPanel sessionId="worker" />
    </QueryClientProvider>,
  );
}
afterEach(cleanup);
beforeEach(() => {
  get.mockReset().mockResolvedValue({ data: snapshot });
  post.mockReset().mockResolvedValue({ data: snapshot });
  decide.mockReset().mockResolvedValue({ status: "recorded" });
  desktop.available = true;
});
describe("LocalWorkflowPanel", () => {
  it("shows required findings and requires an explicit greater limit to resume", async () => {
    show();
    expect(await screen.findByText("Required findings remain")).toBeVisible();
    expect(screen.getByText("Validate empty input")).toBeVisible();
    const resume = screen.getByRole("button", { name: "Resume workflow" });
    expect(resume).toBeDisabled();
    fireEvent.change(
      screen.getByRole("spinbutton", { name: "New round limit" }),
      { target: { value: "5" } },
    );
    fireEvent.click(resume);
    await waitFor(() =>
      expect(post).toHaveBeenCalledWith(
        "/api/v1/sessions/{sessionId}/local-workflow/resume",
        expect.objectContaining({ body: { expectedVersion: 4, maxRounds: 5 } }),
      ),
    );
  });
  it("keeps a start request id across a failed retry", async () => {
    get.mockResolvedValue({
      data: { ...snapshot, run: null, requiredFindings: [] },
    });
    post
      .mockResolvedValueOnce({ error: { message: "Connection lost" } })
      .mockResolvedValueOnce({ data: snapshot });
    show();
    fireEvent.click(
      await screen.findByRole("button", { name: "Start workflow" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Connection lost",
    );
    fireEvent.click(screen.getByRole("button", { name: "Start workflow" }));
    await waitFor(() => expect(post).toHaveBeenCalledTimes(2));
    expect(post.mock.calls[0][1].body.requestId).toBe(
      post.mock.calls[1][1].body.requestId,
    );
  });
  it("shows failed test output and the truncation indicator", async () => {
    get.mockImplementation((url: string) =>
      Promise.resolve({
        data: url.endsWith("/logs")
          ? { text: "Assertion failed", truncated: true }
          : {
              ...snapshot,
              run: { ...run, blockCode: "tests_failed" },
              tests: [
                {
                  id: "test",
                  command: "npm test",
                  status: "failed",
                  exitCode: 1,
                },
              ],
            },
      }),
    );
    show();
    fireEvent.click(await screen.findByRole("button", { name: "Show stdout" }));
    expect(await screen.findByText("Assertion failed")).toBeVisible();
    expect(screen.getByText("Output truncated")).toBeVisible();
  });
  it("stops the current version and reports API failures", async () => {
    get.mockResolvedValue({
      data: {
        ...snapshot,
        run: {
          ...run,
          phase: "testing",
          blockCode: undefined,
          blockMessage: undefined,
        },
      },
    });
    post.mockResolvedValue({ error: { message: "Workflow changed; refresh" } });
    show();
    fireEvent.click(
      await screen.findByRole("button", { name: "Stop workflow" }),
    );
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Workflow changed; refresh",
    );
    expect(post).toHaveBeenCalledWith(
      "/api/v1/sessions/{sessionId}/local-workflow/stop",
      expect.objectContaining({ body: { expectedVersion: 4 } }),
    );
  });
});

it("keeps tests, review findings and logs attached to their round after reopening", async () => {
  const evidence = {
    ...snapshot,
    operations: [
      {
        id: "t1",
        kind: "tests",
        cycle: 1,
        attempt: 1,
        state: "succeeded",
        startedAt: run.createdAt,
      },
      {
        id: "r1",
        kind: "review",
        cycle: 1,
        attempt: 1,
        state: "succeeded",
        reviewRunId: "review1",
        snapshotId: "content1",
        startedAt: run.createdAt,
      },
      {
        id: "t2",
        kind: "tests",
        cycle: 2,
        attempt: 1,
        state: "failed",
        startedAt: run.createdAt,
      },
    ],
    tests: [
      {
        id: "test1",
        operationId: "t1",
        command: "npm test first",
        status: "succeeded",
        exitCode: 0,
      },
      {
        id: "test2",
        operationId: "t2",
        command: "npm test second",
        status: "failed",
        exitCode: 7,
      },
    ],
    reviews: [
      {
        id: "review1",
        harness: "claude-code",
        status: "complete",
        verdict: "changes_requested",
        localResult: {
          summary: "Input is not validated",
          findings: [
            {
              kind: "required",
              path: "input.ts",
              line: 7,
              reason: "Reject empty input",
            },
            {
              kind: "suggestion",
              path: "input.ts",
              line: 9,
              reason: "Optional naming improvement",
            },
          ],
        },
      },
    ],
  };
  get.mockImplementation((url: string) =>
    Promise.resolve({
      data: url.endsWith("/logs")
        ? { text: "round one output", truncated: false }
        : evidence,
    }),
  );
  const first = show();
  const round1 = await screen.findByRole("region", { name: "Round 1" });
  expect(within(round1).getByText("npm test first")).toBeVisible();
  expect(within(round1).getByText("Reject empty input")).toBeVisible();
  expect(within(round1).getByText("Optional naming improvement")).toBeVisible();
  expect(within(round1).getByText("content1")).toBeVisible();
  const round2 = screen.getByRole("region", { name: "Round 2" });
  expect(within(round2).getByText("npm test second")).toBeVisible();
  expect(within(round2).queryByText("Reject empty input")).toBeNull();
  fireEvent.click(within(round1).getByRole("button", { name: "Show stdout" }));
  expect(await screen.findByText("round one output")).toBeVisible();
  first.unmount();
  show();
  expect(
    await screen.findByRole("region", { name: "Round 1" }),
  ).toHaveTextContent("Input is not validated");
});

it("does not show a late log response in another session", async () => {
  let resolveLog!: (value: unknown) => void;
  get.mockImplementation((url: string) =>
    url.endsWith("/logs")
      ? new Promise((resolve) => {
          resolveLog = resolve;
        })
      : Promise.resolve({
          data: {
            ...snapshot,
            tests: [
              {
                id: "old-test",
                command: "npm test",
                status: "failed",
                exitCode: 1,
              },
            ],
          },
        }),
  );
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const panel = (sessionId: string) => (
    <QueryClientProvider client={client}>
      <LocalWorkflowPanel sessionId={sessionId} />
    </QueryClientProvider>
  );
  const view = render(panel("worker"));
  fireEvent.click(await screen.findByRole("button", { name: "Show stdout" }));
  await waitFor(() => expect(resolveLog).toBeDefined());
  view.rerender(panel("other-worker"));
  await act(async () =>
    resolveLog({
      data: { text: "Private previous session output", truncated: false },
    }),
  );
  expect(screen.queryByText("Private previous session output")).toBeNull();
});

const readyForDecision = () => ({
  ...snapshot,
  run: { ...run, phase: "ready", blockCode: "", blockMessage: "", cycle: 1 },
  current: true,
  accepted: false,
  requiredFindings: [],
  decisions: [],
  reviews: [],
  operations: [
    {
      id: "tests",
      kind: "tests",
      cycle: 1,
      state: "succeeded",
      snapshotId: "snapshot",
      attempt: 1,
    },
    {
      id: "review-op",
      kind: "review",
      cycle: 1,
      state: "succeeded",
      snapshotId: "snapshot",
      inputOperationId: "tests",
      reviewRunId: "review",
      attempt: 1,
    },
  ],
});

describe("desktop user decision controls", () => {
  it("requires a comment for corrections and sends exact evidence through preload", async () => {
    get.mockResolvedValue({ data: readyForDecision() });
    show();
    const accept = await screen.findByRole("button", {
      name: "Accept changes",
    });
    const reject = screen.getByRole("button", { name: "Request changes" });
    expect(reject).toBeDisabled();
    fireEvent.change(
      screen.getByRole("textbox", { name: "Decision comment" }),
      { target: { value: "Also cover empty input" } },
    );
    expect(reject).toBeEnabled();
    fireEvent.click(reject);
    await waitFor(() =>
      expect(decide).toHaveBeenCalledWith(
        expect.objectContaining({
          sessionId: "worker",
          workflowId: "wf",
          expectedVersion: 4,
          kind: "changes_requested",
          comment: "Also cover empty input",
          snapshotId: "snapshot",
          testOperationId: "tests",
          reviewRunId: "review",
        }),
      ),
    );
    expect(post).not.toHaveBeenCalled();
    expect(accept).toBeEnabled();
  });
  it("preserves request identity on uncertain retry and replaces it after a changed comment", async () => {
    get.mockResolvedValue({ data: readyForDecision() });
    decide.mockRejectedValue(new Error("Connection lost"));
    show();
    const button = await screen.findByRole("button", {
      name: "Accept changes",
    });
    fireEvent.click(button);
    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Connection lost",
    );
    fireEvent.click(button);
    await waitFor(() => expect(decide).toHaveBeenCalledTimes(2));
    expect(decide.mock.calls[0][0].requestId).toBe(
      decide.mock.calls[1][0].requestId,
    );
    await waitFor(() => expect(button).toBeEnabled());
    fireEvent.change(
      screen.getByRole("textbox", { name: "Decision comment" }),
      { target: { value: "New decision input" } },
    );
    fireEvent.click(button);
    await waitFor(() => expect(decide).toHaveBeenCalledTimes(3));
    expect(decide.mock.calls[2][0].requestId).not.toBe(
      decide.mock.calls[0][0].requestId,
    );
  });
  it("does not show a native cancellation as acceptance or an error", async () => {
    get.mockResolvedValue({ data: readyForDecision() });
    decide.mockResolvedValue({ status: "cancelled" });
    show();
    fireEvent.click(
      await screen.findByRole("button", { name: "Accept changes" }),
    );
    await waitFor(() => expect(decide).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("alert")).toBeNull();
    expect(screen.queryByText("Accepted by you")).toBeNull();
  });
  it("disables desktop decisions for browser preview and stale evidence", async () => {
    desktop.available = false;
    get.mockResolvedValue({ data: readyForDecision() });
    show();
    expect(
      await screen.findByRole("button", { name: "Accept changes" }),
    ).toBeDisabled();
    expect(
      screen.getByText("Use the desktop app on this host to make a decision."),
    ).toBeVisible();
    expect(decide).not.toHaveBeenCalled();
  });
  it("does not carry a pending decision response or comment into another session", async () => {
    get.mockImplementation(
      (_url: string, opts: { params: { path: { sessionId: string } } }) =>
        Promise.resolve({
          data: {
            ...readyForDecision(),
            run: {
              ...readyForDecision().run,
              sessionId: opts.params.path.sessionId,
            },
          },
        }),
    );
    let finish: (value: { status: string }) => void = () => {};
    decide.mockImplementation(
      () =>
        new Promise((resolve) => {
          finish = resolve;
        }),
    );
    const client = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
        mutations: { retry: false },
      },
    });
    const panel = (sessionId: string, hostId?: string) => (
      <QueryClientProvider client={client}>
        <LocalWorkflowPanel sessionId={sessionId} hostId={hostId} />
      </QueryClientProvider>
    );
    const view = render(panel("worker"));
    const comment = await screen.findByRole("textbox", {
      name: "Decision comment",
    });
    fireEvent.change(comment, { target: { value: "Old session feedback" } });
    fireEvent.click(screen.getByRole("button", { name: "Accept changes" }));
    await waitFor(() => expect(decide).toHaveBeenCalledTimes(1));
    view.rerender(panel("other"));
    await waitFor(() =>
      expect(
        screen.getByRole("textbox", { name: "Decision comment" }),
      ).toHaveValue(""),
    );
    await act(async () => finish({ status: "recorded" }));
    expect(screen.queryByText("Accepted by you")).toBeNull();
    view.rerender(panel("other", "remote"));
    expect(
      await screen.findByRole("button", { name: "Accept changes" }),
    ).toBeDisabled();
  });
  it("keeps a stale acceptance in history without claiming current acceptance", async () => {
    get.mockResolvedValue({
      data: {
        ...readyForDecision(),
        current: false,
        accepted: false,
        decisions: [
          {
            id: "decision",
            workflowId: "wf",
            kind: "accepted",
            comment: "Reviewed before editing",
            cycle: 1,
            snapshotId: "old-snapshot",
            createdAt: "2026-10-17T12:00:00Z",
          },
        ],
      },
    });
    show();
    expect(await screen.findByText("Historical acceptance")).toBeVisible();
    expect(screen.getByText("Reviewed before editing")).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Accept changes" }),
    ).toBeDisabled();
  });
});
