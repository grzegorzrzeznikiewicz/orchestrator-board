// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import {
  type UseQueryOptions,
  useMutation,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { useRef } from "react";
import {
  getLocalWorkflow,
  getLocalWorkflowSummaries,
  type LocalWorkflowCard,
  startLocalWorkflow,
  stopLocalWorkflow,
  resumeLocalWorkflow,
  type LocalWorkflowSnapshot,
} from "../lib/local-workflow-api";
import { getEventsConnectionState } from "../lib/events-connection";
import { aoBridge } from "../lib/bridge";

export const localWorkflowQueryKey = (sessionId: string, hostId?: string) =>
  hostId
    ? (["remote-local-workflow", hostId, sessionId] as const)
    : (["local-workflow", sessionId] as const);
type DecisionAction = {
  kind: "accept" | "request_changes";
  version: number;
  workflowId: string;
  snapshotId: string;
  testOperationId: string;
  reviewRunId: string;
  comment: string;
};
type Action =
  | { kind: "start" }
  | { kind: "stop"; version: number }
  | { kind: "resume"; version: number; maxRounds?: number }
  | DecisionAction;
type OwnedAction = {
  sessionId: string;
  hostId?: string;
  input: Action;
  requestKey: string;
  requestId: string;
};

export function useLocalWorkflow(sessionId: string, hostId?: string) {
  const client = useQueryClient();
  const requests = useRef(new Map<string, string>());
  const query = useQuery(localWorkflowQueryOptions(sessionId, hostId));
  const mutation = useMutation({
    mutationFn: async (
      owned: OwnedAction,
    ): Promise<LocalWorkflowSnapshot | undefined> => {
      const { sessionId, hostId, input, requestId } = owned;
      if (input.kind === "start")
        return startLocalWorkflow(sessionId, requestId, hostId);
      if (input.kind === "stop")
        return stopLocalWorkflow(sessionId, input.version, hostId);
      if (input.kind === "resume")
        return resumeLocalWorkflow(
          sessionId,
          input.version,
          input.maxRounds,
          hostId,
        );
      if (hostId || !aoBridge.localWorkflow.available)
        throw new Error("Use the desktop app on this host to make a decision.");
      await aoBridge.localWorkflow.decide({
        sessionId,
        requestId,
        workflowId: input.workflowId,
        expectedVersion: input.version,
        kind: input.kind === "accept" ? "accepted" : "changes_requested",
        comment: input.comment,
        snapshotId: input.snapshotId,
        testOperationId: input.testOperationId,
        reviewRunId: input.reviewRunId,
      });
      // The query is authoritative even when the native dialog was cancelled.
      return undefined;
    },
    onSuccess: (data, owned) => {
      if (data)
        client.setQueryData(
          localWorkflowQueryKey(owned.sessionId, owned.hostId),
          data,
        );
      requests.current.delete(owned.requestKey);
    },
    onSettled: (_data, _error, owned) => {
      void client.invalidateQueries({
        queryKey: localWorkflowQueryKey(owned.sessionId, owned.hostId),
      });
      invalidateLocalWorkflowSummaries(client, owned.sessionId, owned.hostId);
    },
  });
  const owned =
    mutation.variables?.sessionId === sessionId &&
    mutation.variables.hostId === hostId;
  return {
    query,
    action: {
      ...mutation,
      error: owned ? mutation.error : null,
      isPending: owned && mutation.isPending,
      mutate: (input: Action) => {
        const requestKey = JSON.stringify([sessionId, hostId ?? null, input]);
        let requestId = requests.current.get(requestKey);
        if (!requestId) {
          requestId = crypto.randomUUID();
          requests.current.set(requestKey, requestId);
        }
        mutation.mutate({ sessionId, hostId, input, requestId, requestKey });
      },
    },
  };
}

export function localWorkflowQueryOptions(
  sessionId: string,
  hostId?: string,
): UseQueryOptions<LocalWorkflowSnapshot, Error> {
  return {
    queryKey: localWorkflowQueryKey(sessionId, hostId),
    queryFn: () => getLocalWorkflow(sessionId, hostId),
    refetchInterval: (q) =>
      q.state.data?.automation?.pending ||
      (q.state.data?.run &&
        [
          "waiting_executor",
          "testing",
          "reviewing",
          "fixing",
          "stopping",
        ].includes(q.state.data.run.phase))
        ? 1000
        : 10000,
    refetchOnReconnect: "always",
  };
}

// One board request owns the visible session IDs. Full detail queries remain
// separate, so card refreshes cannot fetch evidence or enable a user decision.
export const localWorkflowSummaryRoot = ["local-workflow-summaries"] as const;
export const localWorkflowSummaryKey = (sessionIds: string[], hostId?: string) =>
  [...localWorkflowSummaryRoot, hostId ?? "local", [...new Set(sessionIds)].sort()] as const;

export function invalidateLocalWorkflowSummaries(
  client: ReturnType<typeof useQueryClient>, sessionId?: string, hostId?: string,
) {
  const host = hostId ?? "local";
  void client.invalidateQueries({
    queryKey: localWorkflowSummaryRoot,
    predicate: (query) => query.queryKey[1] === host &&
      (!sessionId || (Array.isArray(query.queryKey[2]) && query.queryKey[2].includes(sessionId))),
  }, { cancelRefetch: false });
}

export function localWorkflowSummaryOptions(sessionIds: string[], hostId?: string): UseQueryOptions<LocalWorkflowCard[], Error> {
  const ids = [...new Set(sessionIds)].sort();
  return {
    queryKey: localWorkflowSummaryKey(ids, hostId),
    queryFn: () => getLocalWorkflowSummaries(ids, hostId),
    enabled: ids.length > 0,
    placeholderData: (previous, previousQuery) =>
      previousQuery?.queryKey[1] === (hostId ?? "local")
        ? previous?.filter((card) => ids.includes(card.sessionId))
        : undefined,
    staleTime: 10_000,
    refetchInterval: (query) => {
      const active = query.state.data?.some((card) => card.automation?.pending ||
        (card.run && ["waiting_executor", "testing", "reviewing", "fixing", "stopping"].includes(card.run.phase)));
      // SSE handles transitions; polling also observes workspace-only changes
      // and recovers from dropped events. It stays one batch at either cadence.
      return active ? 5_000 : (hostId || getEventsConnectionState() !== "connected" ? 15_000 : 30_000);
    },
    refetchOnReconnect: "always",
  };
}
