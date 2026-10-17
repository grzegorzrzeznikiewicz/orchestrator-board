import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useRef } from "react";
import {
  getLocalWorkflow,
  startLocalWorkflow,
  stopLocalWorkflow,
  resumeLocalWorkflow,
} from "../lib/local-workflow-api";

export const localWorkflowQueryKey = (sessionId: string, hostId?: string) =>
  hostId
    ? (["remote-local-workflow", hostId, sessionId] as const)
    : (["local-workflow", sessionId] as const);
type Action =
  | { kind: "start" }
  | { kind: "stop"; version: number }
  | { kind: "resume"; version: number; maxRounds?: number };
export function useLocalWorkflow(sessionId: string, hostId?: string) {
  const client = useQueryClient();
  const key = localWorkflowQueryKey(sessionId, hostId);
  const requestId = useRef<string | null>(null);
  const query = useQuery({
    queryKey: key,
    queryFn: () => getLocalWorkflow(sessionId, hostId),
    refetchInterval: (q) =>
      q.state.data?.run &&
      [
        "waiting_executor",
        "testing",
        "reviewing",
        "fixing",
        "stopping",
      ].includes(q.state.data.run.phase)
        ? 1000
        : 10000,
    refetchOnReconnect: "always",
  });
  const action = useMutation({
    mutationFn: (input: Action) => {
      if (input.kind === "start") {
        requestId.current ??= crypto.randomUUID();
        return startLocalWorkflow(sessionId, requestId.current, hostId);
      }
      if (input.kind === "stop")
        return stopLocalWorkflow(sessionId, input.version, hostId);
      return resumeLocalWorkflow(
        sessionId,
        input.version,
        input.maxRounds,
        hostId,
      );
    },
    onSuccess: (data, input) => {
      client.setQueryData(key, data);
      if (input.kind === "start") requestId.current = null;
    },
    onSettled: () => {
      void client.invalidateQueries({ queryKey: key });
    },
  });
  return { query, action };
}
