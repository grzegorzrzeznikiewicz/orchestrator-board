// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

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
  it.each(["provider_execution_uncertain", "provider_uncertain", "review_uncertain"])("keeps %s visible and prevents Resume without retry metadata", async (blockCode) => {
    get.mockResolvedValue({data:{...snapshot,run:{...run,blockCode,blockMessage:"Previous provider termination is unconfirmed"}}});
    show();
    expect(await screen.findByText("Previous provider termination is unconfirmed")).toBeVisible();
    const resume=screen.getByRole("button",{name:"Resume workflow"});
    expect(resume).toBeDisabled();
    fireEvent.click(resume);
    expect(post).not.toHaveBeenCalled();
  });
  it("keeps stopped provider uncertainty actionable without promising a safe Resume", async () => {
    get.mockResolvedValue({data:{...snapshot,run:{...run,stopRequested:true,blockCode:"provider_execution_uncertain",blockMessage:"Previous provider termination is unconfirmed",providerRetry:{stage:"planner",state:"uncertain",reason:"authentication",attempt:1,maxAttempts:3,failureAt:"2026-10-20T10:00:00Z"}}}});
    show();
    expect(await screen.findByText("End the affected session and confirm that its provider process has ended before starting another task.")).toBeVisible();
    expect(screen.queryByText("Check provider access and resume manually when ready.")).toBeNull();
    const resume=screen.getByRole("button",{name:"Resume workflow"});
    expect(resume).toBeDisabled();
    fireEvent.click(resume);
    expect(post).not.toHaveBeenCalled();
    expect(decide).not.toHaveBeenCalled();
  });
  it("shows durable provider waiting facts while keeping Stop explicit and acceptance unavailable", async () => {
    get.mockResolvedValue({data:{...snapshot,run:{...run,blockCode:"provider_waiting",blockMessage:"Raw provider text",providerRetry:{stage:"review",state:"waiting",reason:"authentication",attempt:1,maxAttempts:3,failureAt:"2026-10-20T10:00:00Z",nextAt:"2026-10-20T10:02:00Z"}}}});
    show();
    expect(await screen.findByText("Waiting for provider access")).toBeVisible();
    expect(screen.getByText("Provider authentication needs recovery")).toBeVisible();
    expect(screen.getByText(/^Next attempt:/)).toHaveAttribute("datetime","2026-10-20T10:02:00Z");
    expect(screen.queryByText("Raw provider text")).toBeNull();
    expect(screen.queryByRole("button",{name:"Accept changes"})).toBeNull();
    expect(post).not.toHaveBeenCalled();
    expect(decide).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole("button",{name:"Stop workflow"}));
    await waitFor(()=>expect(post).toHaveBeenCalledWith("/api/v1/sessions/{sessionId}/local-workflow/stop",expect.objectContaining({body:{expectedVersion:4}})));
  });
  it("retains provider recovery facts in previous workflow history without scheduling them again", async () => {
    get.mockResolvedValue({data:{...snapshot,previousRuns:[{...run,id:"prior-retry",providerRetry:{stage:"planner",state:"waiting",reason:"unavailable",attempt:2,maxAttempts:3,failureAt:"2026-10-20T10:00:00Z",lastAt:"2026-10-20T10:01:00Z",nextAt:"2026-10-20T10:02:00Z"}}]}});
    show();
    fireEvent.click(await screen.findByText("Previous runs (1)"));
    const history=screen.getByRole("region",{name:"Provider recovery history"});
    expect(within(history).getByText("Automatic retries: 2 of 3")).toBeVisible();
    expect(within(history).getByText("Previous issue: Provider is temporarily unavailable")).toBeVisible();
    expect(within(history).queryByText(/^Next attempt:/)).toBeNull();
    expect(post).not.toHaveBeenCalled();
  });
  it("shows automatic preparation without requiring a manual start", async () => {
    get.mockResolvedValue({
      data: { ...snapshot, run: null, automation: { pending: true }, requiredFindings: [] },
    });
    show();
    expect(await screen.findByText("Preparing automatic verification…")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Start workflow" })).toBeNull();
    expect(post).not.toHaveBeenCalled();
  });
  it("explains an automatic start waiting for an uncertain previous operation", async () => {
    get.mockResolvedValue({
      data: { ...readyForDecision(), automation: { pending: true, blockMessage: "The previous test process has no confirmed completion" }, current: false, accepted: false },
    });
    show();
    expect(await screen.findByRole("alert")).toHaveTextContent("The previous test process has no confirmed completion");
    expect(screen.getByText("Preparing automatic verification…")).toBeVisible();
    expect(screen.getByRole("region", { name: "Round history" })).toBeVisible();
    expect(screen.queryByRole("button", { name: "Accept changes" })).toBeNull();
    expect(screen.queryByRole("button", { name: "Start workflow" })).toBeNull();
  });
  it("keeps previous workflow summaries available without claiming current acceptance", async () => {
    get.mockResolvedValue({
      data: { ...snapshot, previousRuns: [{ ...run, id: "old-run", phase: "stopped", cycle: 2, closedAt: run.updatedAt, blockCode: "", blockMessage: "", verificationPlan: { profile: "report", rationale: "Report", commands: [], checks: ["Scope"] } }, { ...run, id: "blocked-run", blockMessage: "Previous verification lacked evidence" }] },
    });
    show();
    const summary = await screen.findByText("Previous runs (2)");
    const history = summary.closest("details")!;
    expect(history).not.toHaveAttribute("open");
    fireEvent.click(summary);
    expect(history).toHaveAttribute("open");
    expect(within(history).getByText("old-run")).toBeVisible();
    expect(within(history).getByText("blocked-run")).toBeVisible();
    expect(within(history).getByText("Report")).toBeVisible();
    expect(within(history).getByText("Cycle 2 of 3")).toBeVisible();
    expect(within(history).getByText("Workflow stopped")).toBeVisible();
    expect(within(history).getByText("Previous verification lacked evidence")).toBeVisible();
    expect(within(history).getAllByRole("time")[0]).toHaveAttribute("dateTime", run.createdAt);
    expect(within(history).queryByText("Accepted by you")).toBeNull();
    expect(within(history).queryByRole("button", { name: "Show stdout" })).toBeNull();
  });
  it.each(["pending", "running"])("shows automatic verification planning while the plan is %s", async (planState) => {
    get.mockResolvedValue({
      data: { ...snapshot, run: { ...run, phase: "waiting_executor", automatic: true, planState, blockCode: "", blockMessage: "" }, requiredFindings: [] },
    });
    show();
    expect(await screen.findByRole("status")).toHaveTextContent("Planning verification");
    expect(screen.queryByText("Waiting for executor")).toBeNull();
  });
  it("keeps a planning failure actionable instead of showing planning indefinitely", async () => {
    get.mockResolvedValue({
      data: { ...snapshot, run: { ...run, automatic: true, planState: "pending", blockCode: "configuration", blockMessage: "Configure a Claude reviewer" }, requiredFindings: [] },
    });
    show();
    expect(await screen.findByRole("status")).toHaveTextContent("Workflow blocked");
    expect(screen.getByText("Configure a Claude reviewer")).toBeVisible();
    expect(screen.getByRole("button", { name: "Resume workflow" })).toBeEnabled();
  });
  it("shows implementation commands and criteria from the verification plan", async () => {
    get.mockResolvedValue({
      data: { ...snapshot, run: { ...run, verificationPlan: { profile: "implementation", rationale: "The task changes the parser", commands: ["npm run test:parser", "npm run lint"], checks: ["Reject malformed input"] } } },
    });
    show();
    const plan = await screen.findByRole("region", { name: "Verification plan" });
    expect(within(plan).getByText("Code changes")).toBeVisible();
    expect(within(plan).getByText("The task changes the parser")).toBeVisible();
    expect(within(plan).getByText("npm run test:parser")).toBeVisible();
    expect(within(plan).getByText("npm run lint")).toBeVisible();
    expect(within(plan).getByText("Reject malformed input")).toBeVisible();
    expect(within(plan).queryByText(/Not applicable/)).toBeNull();
  });
  it("shows report verification without claiming that automated tests passed", async () => {
    const ready = readyForDecision();
    get.mockResolvedValue({
      data: { ...ready, run: { ...ready.run, automatic: true, testCommands: [], verificationPlan: { profile: "report", rationale: "The task produces an assessment only", commands: [], checks: ["Conclusions cite observed evidence"] } } },
    });
    show();
    const plan = await screen.findByRole("region", { name: "Verification plan" });
    expect(within(plan).getByText("Report")).toBeVisible();
    expect(within(plan).getByText("Conclusions cite observed evidence")).toBeVisible();
    expect(within(plan).getByText("Automated tests: not applicable to this report.")).toBeVisible();
    expect(screen.getByText("The reviewer approved this report. Ready for your acceptance.")).toBeVisible();
    expect(screen.queryByText(/Tests passed/)).toBeNull();
    expect(screen.queryByText(/0 of 0 passed/)).toBeNull();
    expect(within(screen.getByRole("region", { name: "Round 1" })).getByText("Automated tests: not applicable to this report.")).toBeVisible();
    expect(screen.getByRole("button", { name: "Accept changes" })).toBeEnabled();
  });
  it("requires completed reviewer evidence even when automated report tests are not applicable", async () => {
    const ready = readyForDecision();
    get.mockResolvedValue({ data: { ...ready, run: { ...ready.run, testCommands: [], verificationPlan: { profile: "report", rationale: "Assessment", commands: [], checks: ["Evidence is cited"] } }, operations: ready.operations.filter((op) => op.kind !== "review") } });
    show();
    expect(await screen.findByRole("button", { name: "Accept changes" })).toBeDisabled();
  });
  it("does not accept an older ready result while a new instruction is awaiting automatic verification", async () => {
    get.mockResolvedValue({
      data: { ...readyForDecision(), automation: { pending: true }, current: false, accepted: false, decisions: [{ id: "previous", kind: "accepted", cycle: 1, snapshotId: "previous-files", createdAt: run.createdAt }] },
    });
    show();
    expect(await screen.findByText("Preparing automatic verification…")).toBeVisible();
    expect(screen.queryByRole("button", { name: "Accept changes" })).toBeNull();
    expect(screen.queryByText("Tests passed and the reviewer approved these files. Ready for your acceptance.")).toBeNull();
    expect(screen.getByText("Historical acceptance")).toBeVisible();
    expect(screen.queryByText("Accepted by you")).toBeNull();
  });
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
  it.each(["recorded", "failed"])(
    "isolates a late local decision when only the host changes: %s",
    async (outcome) => {
      get.mockResolvedValue({ data: readyForDecision() });
      let resolveDecision: (value: { status: string }) => void = () => {};
      let rejectDecision: (error: Error) => void = () => {};
      decide.mockImplementation(
        () => new Promise((resolve, reject) => {
          resolveDecision = resolve;
          rejectDecision = reject;
        }),
      );
      const client = new QueryClient({
        defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
      });
      const panel = (hostId?: string) => (
        <QueryClientProvider client={client}>
          <LocalWorkflowPanel sessionId="worker" hostId={hostId} />
        </QueryClientProvider>
      );
      const view = render(panel());
      fireEvent.change(await screen.findByRole("textbox", { name: "Decision comment" }), {
        target: { value: "Only for this local host" },
      });
      fireEvent.click(screen.getByRole("button", { name: "Accept changes" }));
      await waitFor(() => expect(decide).toHaveBeenCalledTimes(1));

      view.rerender(panel("other-host"));
      await waitFor(() => expect(
        screen.getByRole("textbox", { name: "Decision comment" }),
      ).toHaveValue(""));
      expect(screen.getByRole("button", { name: "Accept changes" })).toBeDisabled();
      await act(async () => {
        if (outcome === "recorded") resolveDecision({ status: "recorded" });
        else rejectDecision(new Error("Previous local host disconnected"));
      });
      expect(screen.queryByText("Accepted by you")).toBeNull();
      expect(screen.queryByRole("alert")).toBeNull();
      expect(screen.getByRole("textbox", { name: "Decision comment" })).toHaveValue("");
      expect(screen.getByRole("button", { name: "Accept changes" })).toBeDisabled();
      expect(decide).toHaveBeenCalledTimes(1);
    },
  );
});
