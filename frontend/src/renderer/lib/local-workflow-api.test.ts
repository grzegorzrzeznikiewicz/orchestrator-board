// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).
import { expect, it, vi } from "vitest";
import { getLocalWorkflowSummaries } from "./local-workflow-api";
const { get }=vi.hoisted(()=>({get:vi.fn()}));
vi.mock("./host-clients",()=>({clientForSessionHost:()=>({GET:get})}));
it("keeps large boards within the endpoint batch limit",async()=>{
 get.mockImplementation(async(_path:string,options:{params:{query:{sessionIds:string}}})=>{
  const ids=options.params.query.sessionIds.split(",");
  return ids.length>200 ? {error:{message:"Batch limit exceeded"}} : {data:{summaries:ids.map(sessionId=>({sessionId}))}};
 });
 const ids=Array.from({length:201},(_,i)=>`worker-${i}`);
 const cards=await getLocalWorkflowSummaries(ids,"remote");
 expect(cards.map(card=>card.sessionId)).toEqual(ids);
 expect(get).toHaveBeenCalledTimes(2);
 expect(get.mock.calls.every(([path,options])=>path==="/api/v1/local-workflow/summaries" && options.params.query.sessionIds.split(",").length<=200)).toBe(true);
});
