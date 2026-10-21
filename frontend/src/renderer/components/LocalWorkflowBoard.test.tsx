// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, render, screen, within } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { LocalWorkflowBoard } from "./LocalWorkflowBoard";
import type { WorkspaceSession } from "../types/workspace";
const { get, forHost } = vi.hoisted(() => ({ get: vi.fn(), forHost: vi.fn() }));
vi.mock("../lib/host-clients", () => ({
 clientForSessionHost: (hostId?: string) => { forHost(hostId); return { GET: get }; },
}));
afterEach(() => { cleanup(); vi.clearAllMocks(); });
const session: WorkspaceSession = {id:"worker",title:"Task without PR",provider:"codex",workspaceId:"p",workspaceName:"Project",prs:[],status:"approved",kanbanColumn:"ready",updatedAt:"2026-10-17T12:00:00Z"};
function show(sessions=[session],hostId?:string) {
 return render(<QueryClientProvider client={new QueryClient({defaultOptions:{queries:{retry:false}}})}><LocalWorkflowBoard sessions={sessions} hostId={hostId} renderSessionCard={(s,details)=><article>{s.title}{details}</article>}/></QueryClientProvider>);
}
function respond(card: Record<string,unknown>) {get.mockResolvedValue({data:{summaries:[{sessionId:"worker",current:false,accepted:false,testsPresent:false,testsPassed:0,testsTotal:0,requiredCount:0,automation:{pending:false},...card}]}});}
describe("local workflow lanes",()=>{
 it("shows scheduled provider recovery from the batch without loading full workflow evidence",async()=>{
  respond({run:{id:"wf",phase:"blocked",cycle:1,maxRounds:3,blockCode:"provider_waiting",blockMessage:"Raw provider text",providerRetry:{stage:"planner",state:"waiting",reason:"rate_limited",attempt:0,maxAttempts:3,failureAt:"2026-10-20T10:00:00Z",nextAt:"2026-10-20T10:02:00Z"}},stage:"executing"});show();
  expect(await screen.findByText("Waiting for provider access")).toBeVisible();
  expect(screen.getByText("Provider rate limit reached")).toBeVisible();
  expect(screen.getByText("Automatic retries: 0 of 3")).toBeVisible();
  expect(screen.getByText(/^Next attempt:/)).toHaveAttribute("datetime","2026-10-20T10:02:00Z");
  expect(screen.queryByText("Raw provider text")).toBeNull();
  expect(screen.queryByText("provider_waiting")).toBeNull();
  expect(get).toHaveBeenCalledTimes(1);
  expect(get.mock.calls[0][0]).toBe("/api/v1/local-workflow/summaries");
 });
 it("loads N cards with one batch and never reads full evidence",async()=>{
  get.mockResolvedValue({data:{summaries:[{sessionId:"worker",stage:"executing",run:null},{sessionId:"second",stage:"testing",run:{id:"wf",phase:"testing",cycle:1,maxRounds:3}}]}});
  show([session,{...session,id:"second",title:"Second task"}],"remote-host");
  expect(await screen.findByText("Workflow not started")).toBeVisible();
  expect(await within(screen.getByRole("region",{name:"Tests sessions"})).findByText("Second task")).toBeVisible();
  expect(get).toHaveBeenCalledTimes(1);
  expect(get).toHaveBeenCalledWith("/api/v1/local-workflow/summaries",{params:{query:{sessionIds:"second,worker"}}});
  expect(forHost).toHaveBeenCalledWith("remote-host");
 });
 it("shows automatic preparation instead of the older accepted result",async()=>{
  respond({run:{id:"wf",phase:"ready",cycle:1,maxRounds:3},automation:{pending:true},stage:"executing"});show();
  expect(await screen.findByText("Preparing automatic verification…")).toBeVisible();
  expect(screen.queryByText("Ready for your acceptance")).toBeNull();
 });
 it("shows the preparation block reason",async()=>{
  respond({run:null,automation:{pending:true,blockMessage:"Waiting for the previous test process to stop"},stage:"executing"});show();
  expect(await screen.findByRole("alert")).toHaveTextContent("Waiting for the previous test process to stop");
 });
 it("shows automatic planning",async()=>{
  respond({run:{id:"wf",phase:"waiting_executor",automatic:true,planState:"running",cycle:1,maxRounds:3},stage:"executing"});show();
  expect(await screen.findByText("Planning verification")).toBeVisible();
 });
 it("labels report tests as not applicable",async()=>{
  respond({run:{id:"wf",phase:"reviewing",cycle:1,maxRounds:3,profile:"report"},stage:"local_review",testsPresent:true,testsTotal:0});show();
  expect(await screen.findByText("Report")).toBeVisible();
  expect(screen.getByText("Automated tests: not applicable to this report.")).toBeVisible();
  expect(screen.queryByText(/0 of 0 passed/)).toBeNull();
 });
 it.each([["waiting_executor","executing","Execution"],["testing","testing","Tests"],["reviewing","local_review","Local review"],["fixing","fixing","Corrections"],["ready","awaiting_acceptance","Awaiting acceptance"],["blocked","fixing","Corrections"],["stopped","testing","Tests"]])("uses daemon stage for %s",async(phase,stage,lane)=>{
  respond({run:{id:"wf",phase,cycle:2,maxRounds:3,blockMessage:phase==="blocked"?"Round limit reached":""},stage,current:phase==="ready"});show();
  const column=screen.getByRole("region",{name:`${lane} sessions`});
  expect(await within(column).findByText("Task without PR")).toBeVisible();
  expect(await within(column).findByText("Cycle 2 of 3")).toBeVisible();
 });
 it("shows latest test counts and required correction without full history",async()=>{
  respond({run:{id:"wf",phase:"blocked",cycle:2,maxRounds:3},stage:"testing",testsPresent:true,testsPassed:0,testsTotal:1,requiredCount:1,requiredReason:"Reject empty input"});show();
  expect(await screen.findByText("Tests: 0 of 1 passed")).toBeVisible();
  expect(screen.getByText("1 required finding")).toBeVisible();
  expect(screen.getByText("Reject empty input")).toBeVisible();
 });
 it("keeps stale approval out of awaiting acceptance",async()=>{
  respond({run:{id:"wf",phase:"ready",cycle:2,maxRounds:3},stage:"local_review",current:false});show();
  expect(await within(screen.getByRole("region",{name:"Local review sessions"})).findByText("Task without PR")).toBeVisible();
  expect(screen.getByText("Verification is out of date")).toBeVisible();
 });
 it("displays accepted work as accepted",async()=>{
  respond({run:{id:"wf",phase:"ready",cycle:1,maxRounds:3},stage:"accepted",current:true,accepted:true});show();
  const column=screen.getByRole("region",{name:"Accepted sessions"});
  expect(await within(column).findByText("Task without PR")).toBeVisible();
  expect(within(column).queryByText("Ready for your acceptance")).toBeNull();
  expect(within(column).getAllByText("Accepted")).toHaveLength(2);
 });
 it("shows unavailable data conservatively",async()=>{
  get.mockRejectedValue(new Error("Daemon disconnected"));show();
  expect(await screen.findByRole("alert")).toHaveTextContent("Daemon disconnected");
  expect(within(screen.getByRole("region",{name:"Awaiting acceptance sessions"})).queryByText("Task without PR")).toBeNull();
 });
});

it("keeps existing lane placement while the visible session set grows",async()=>{
 get.mockResolvedValueOnce({data:{summaries:[{sessionId:"worker",run:{id:"wf",phase:"ready",cycle:1,maxRounds:3},automation:{pending:false},stage:"accepted",current:true,accepted:true}]}})
  .mockImplementationOnce(()=>new Promise(()=>{}));
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 const board=(sessions:WorkspaceSession[])=><QueryClientProvider client={client}><LocalWorkflowBoard sessions={sessions} renderSessionCard={(s,details)=><article>{s.title}{details}</article>}/></QueryClientProvider>;
 const view=render(board([session]));
 const accepted=screen.getByRole("region",{name:"Accepted sessions"});
 expect(await within(accepted).findByText("Task without PR")).toBeVisible();
 view.rerender(board([session,{...session,id:"new-worker",title:"New task"}]));
 expect(within(accepted).getByText("Task without PR")).toBeVisible();
});

it("does not carry accepted placement to another host",async()=>{
 get.mockReset().mockResolvedValueOnce({data:{summaries:[{sessionId:"worker",run:{id:"wf",phase:"ready",cycle:1,maxRounds:3},automation:{pending:false},stage:"accepted",current:true,accepted:true}]}})
  .mockImplementationOnce(()=>new Promise(()=>{}));
 const client=new QueryClient({defaultOptions:{queries:{retry:false}}});
 const board=(hostId?:string)=><QueryClientProvider client={client}><LocalWorkflowBoard sessions={[session]} hostId={hostId} renderSessionCard={(s,details)=><article>{s.title}{details}</article>}/></QueryClientProvider>;
 const view=render(board());
 const accepted=screen.getByRole("region",{name:"Accepted sessions"});
 expect(await within(accepted).findByText("Task without PR")).toBeVisible();
 view.rerender(board("another-host"));
 expect(within(accepted).queryByText("Task without PR")).toBeNull();
});

it("keeps unknown verification profiles from rendering untranslated labels",async()=>{
 get.mockReset().mockResolvedValue({data:{summaries:[{sessionId:"worker",run:{id:"wf",phase:"reviewing",cycle:1,maxRounds:3,profile:"future-profile"},automation:{pending:false},stage:"local_review",current:false,accepted:false}]}});
 show();
 expect(await screen.findByText("Cycle 1 of 3")).toBeVisible();
 expect(screen.queryByText("localWorkflow.profile.future-profile")).toBeNull();
 expect(screen.queryByText("Automated tests: not applicable to this report.")).toBeNull();
});
