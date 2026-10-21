// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import type { components } from "../../api/schema";
import { apiErrorMessage } from "./api-client";
import { clientForSessionHost } from "./host-clients";

export type LocalWorkflowSnapshot =
  components["schemas"]["LocalWorkflowResponse"];
export type LocalWorkflowCard = components["schemas"]["LocalWorkflowCardSummary"];
export type LocalWorkflowProviderRetryFacts = components["schemas"]["LocalWorkflowProviderRetry"];

export async function getLocalWorkflowSummaries(sessionIds: string[], hostId?: string): Promise<LocalWorkflowCard[]> {
  const cards: LocalWorkflowCard[] = [];
  for (let offset = 0; offset < sessionIds.length; offset += 200) {
    const response = result(await clientForSessionHost(hostId).GET("/api/v1/local-workflow/summaries", {
      params: { query: { sessionIds: sessionIds.slice(offset, offset + 200).join(",") } },
    }));
    cards.push(...response.summaries);
  }
  return cards;
}

function result<T>({ data, error }: { data?: T; error?: unknown }): T {
  if (error || !data)
    throw new Error(apiErrorMessage(error, "Local workflow request failed"));
  return data;
}
export async function getLocalWorkflow(sessionId: string, hostId?: string) {
  return result(
    await clientForSessionHost(hostId).GET(
      "/api/v1/sessions/{sessionId}/local-workflow",
      { params: { path: { sessionId } } },
    ),
  );
}
export async function startLocalWorkflow(
  sessionId: string,
  requestId: string,
  hostId?: string,
) {
  return result(
    await clientForSessionHost(hostId).POST(
      "/api/v1/sessions/{sessionId}/local-workflow/start",
      { params: { path: { sessionId } }, body: { requestId } },
    ),
  );
}
export async function stopLocalWorkflow(
  sessionId: string,
  expectedVersion: number,
  hostId?: string,
) {
  return result(
    await clientForSessionHost(hostId).POST(
      "/api/v1/sessions/{sessionId}/local-workflow/stop",
      { params: { path: { sessionId } }, body: { expectedVersion } },
    ),
  );
}
export async function resumeLocalWorkflow(
  sessionId: string,
  expectedVersion: number,
  maxRounds?: number,
  hostId?: string,
) {
  return result(
    await clientForSessionHost(hostId).POST(
      "/api/v1/sessions/{sessionId}/local-workflow/resume",
      {
        params: { path: { sessionId } },
        body: {
          expectedVersion,
          ...(maxRounds === undefined ? {} : { maxRounds }),
        },
      },
    ),
  );
}
export async function getLocalWorkflowTestLog(
  sessionId: string,
  testId: string,
  stream: "stdout" | "stderr",
  hostId?: string,
) {
  return result(
    await clientForSessionHost(hostId).GET(
      "/api/v1/sessions/{sessionId}/local-workflow/tests/{testId}/logs",
      { params: { path: { sessionId, testId }, query: { stream } } },
    ),
  );
}
