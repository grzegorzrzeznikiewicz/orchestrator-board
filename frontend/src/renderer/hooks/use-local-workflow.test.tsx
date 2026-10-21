// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, cleanup, render, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { useLocalWorkflow } from "./use-local-workflow";

const { get } = vi.hoisted(() => ({ get: vi.fn() }));
vi.mock("../lib/host-clients", () => ({
  clientForSessionHost: () => ({ GET: get }),
}));
vi.mock("../lib/bridge", () => ({
  aoBridge: { localWorkflow: { available: false } },
}));

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

it("refreshes automatic preparation promptly before a workflow run exists", async () => {
  vi.useFakeTimers();
  get.mockResolvedValueOnce({ data: { run: null, automation: { pending: true }, current: false, stage: "executing", operations: [], tests: [], reviews: [], requiredFindings: [] } })
    .mockResolvedValue({ data: { run: { id: "wf", phase: "waiting_executor", automatic: true, planState: "running", cycle: 1, maxRounds: 3 }, automation: { pending: false }, current: false, stage: "executing", operations: [], tests: [], reviews: [], requiredFindings: [] } });
  function Status() {
    const { query } = useLocalWorkflow("worker");
    return <p>{query.data?.run?.id ?? "No run"}</p>;
  }
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  await act(async () => {
    render(<QueryClientProvider client={client}><Status /></QueryClientProvider>);
  });
  await act(async () => { await vi.advanceTimersByTimeAsync(0); });
  expect(screen.getByText("No run")).toBeVisible();
  await act(async () => { await vi.advanceTimersByTimeAsync(1001); });
  expect(screen.getByText("wf")).toBeVisible();
  client.clear();
});

it("keeps board refreshes batched and slower than open evidence polling", async () => {
 vi.useFakeTimers();
 get.mockReset().mockResolvedValue({data:{summaries:[{sessionId:"one",run:{id:"wf",phase:"testing"},automation:{pending:false}}]}});
 const {localWorkflowSummaryOptions}=await import("./use-local-workflow");
 const {useQuery}=await import("@tanstack/react-query");
 function BoardStatus(){ const q=useQuery(localWorkflowSummaryOptions(["one","two"])); return <p>{q.data?.length ?? 0} cards</p>; }
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 await act(async()=>{render(<QueryClientProvider client={client}><BoardStatus/></QueryClientProvider>);await vi.advanceTimersByTimeAsync(0);});
 expect(get).toHaveBeenCalledTimes(1);
 await act(async()=>{await vi.advanceTimersByTimeAsync(4_999);});
 expect(get).toHaveBeenCalledTimes(1);
 await act(async()=>{await vi.advanceTimersByTimeAsync(1);});
 expect(get).toHaveBeenCalledTimes(2);
 expect(get.mock.calls.every(([path])=>path==="/api/v1/local-workflow/summaries")).toBe(true);
 client.clear();
});
