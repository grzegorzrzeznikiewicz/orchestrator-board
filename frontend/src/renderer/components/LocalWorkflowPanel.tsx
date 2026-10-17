import { useQuery } from "@tanstack/react-query";
import { LocalWorkflowHistory } from "./LocalWorkflowHistory";
import type { LocalWorkflowSnapshot } from "../lib/local-workflow-api";
import { useEffect, useState } from "react";
import { useTranslation } from "react-i18next";
import { useLocalWorkflow } from "../hooks/use-local-workflow";
import { getLocalWorkflowTestLog } from "../lib/local-workflow-api";
import { Button } from "./ui/button";

export function LocalWorkflowPanel({
  sessionId,
  hostId,
}: {
  sessionId: string;
  hostId?: string;
}) {
  const { t } = useTranslation();
  const { query, action } = useLocalWorkflow(sessionId, hostId);
  const data = query.data;
  const run = data?.run;
  const [limit, setLimit] = useState(3);
  const [selectedLog, setSelectedLog] = useState<{
    sessionId: string;
    hostId?: string;
    testId: string;
    command: string;
    stream: "stdout" | "stderr";
  } | null>(null);
  const log =
    selectedLog?.sessionId === sessionId && selectedLog.hostId === hostId
      ? selectedLog
      : null;
  const logQuery = useQuery({
    queryKey: [
      "local-workflow-log",
      hostId ?? "local",
      sessionId,
      log?.testId,
      log?.stream,
    ],
    enabled: !!log,
    queryFn: () =>
      getLocalWorkflowTestLog(sessionId, log!.testId, log!.stream, hostId),
  });
  const output = log ? logQuery.data : undefined;
  useEffect(() => {
    setLimit(run?.maxRounds ?? 3);
  }, [run?.id, run?.blockCode, run?.maxRounds]);
  const error = action.error ?? (log ? logQuery.error : null) ?? query.error;
  const canResume = !!run && ["blocked", "stopped"].includes(run.phase);
  const limited = run?.blockCode === "round_limit";
  function showLog(
    test: NonNullable<LocalWorkflowSnapshot["tests"]>[number],
    stream: "stdout" | "stderr",
  ) {
    setSelectedLog({
      sessionId,
      hostId,
      testId: test.id,
      command: test.command,
      stream,
    });
  }
  return (
    <section
      className="space-y-3 rounded-md border border-border p-3 text-xs"
      aria-label={t("localWorkflow.title")}
    >
      <h3 className="font-medium">{t("localWorkflow.title")}</h3>
      {error && (
        <p role="alert" className="text-destructive">
          {error.message}
        </p>
      )}
      {query.isLoading && <p>{t("localWorkflow.loading")}</p>}
      {!query.isLoading && !run && !query.error && (
        <Button
          size="sm"
          disabled={action.isPending}
          onClick={() => action.mutate({ kind: "start" })}
        >
          {t("localWorkflow.start")}
        </Button>
      )}
      {run && (
        <>
          <p role="status">
            {t(`localWorkflow.phase.${run.phase}`)} ·{" "}
            {t("localWorkflow.cycle", {
              cycle: run.cycle,
              limit: run.maxRounds,
            })}
          </p>
          {run.blockCode && (
            <code className="break-all text-warning">{run.blockCode}</code>
          )}
          {run.blockMessage && (
            <p className="text-warning">{run.blockMessage}</p>
          )}
          {run.phase === "ready" && (
            <p>
              {t(
                data?.current
                  ? "localWorkflow.readyDescription"
                  : "localWorkflow.stale",
              )}
            </p>
          )}
          <div className="flex flex-wrap items-center gap-2">
            {!["stopped", "stopping"].includes(run.phase) && (
              <Button
                size="sm"
                variant="outline"
                disabled={action.isPending}
                onClick={() =>
                  action.mutate({ kind: "stop", version: run.version })
                }
              >
                {t("localWorkflow.stop")}
              </Button>
            )}
            {limited && (
              <label>
                {t("localWorkflow.newLimit")}
                <input
                  aria-label={t("localWorkflow.newLimit")}
                  type="number"
                  min={run.maxRounds + 1}
                  step={1}
                  className="ml-2 w-20 rounded border border-input bg-background p-1"
                  value={limit}
                  onChange={(e) => setLimit(e.target.valueAsNumber)}
                />
              </label>
            )}
            {canResume && (
              <Button
                size="sm"
                disabled={
                  action.isPending ||
                  (limited &&
                    (!Number.isInteger(limit) || limit <= run.maxRounds))
                }
                onClick={() =>
                  action.mutate({
                    kind: "resume",
                    version: run.version,
                    ...(limited ? { maxRounds: limit } : {}),
                  })
                }
              >
                {t("localWorkflow.resume")}
              </Button>
            )}
          </div>
          {(data?.requiredFindings ?? []).length > 0 && (
            <ul className="space-y-2">
              {data!.requiredFindings.map((finding, i) => (
                <li key={i}>
                  <code>
                    {finding.path}:{finding.line}
                  </code>
                  <p>{finding.reason}</p>
                </li>
              ))}
            </ul>
          )}
          {data && <LocalWorkflowHistory data={data} onShowLog={showLog} />}
          {log && (
            <p>
              {t("localWorkflow.logTitle", {
                command: log.command,
                stream: log.stream,
              })}
            </p>
          )}
          {log && logQuery.isFetching && <p>{t("localWorkflow.loading")}</p>}
          {output && (
            <div>
              {output.truncated && <p>{t("localWorkflow.truncated")}</p>}
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all rounded bg-muted p-2">
                {output.text}
              </pre>
            </div>
          )}
        </>
      )}
    </section>
  );
}
