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
  const [output, setOutput] = useState<{
    text: string;
    truncated: boolean;
  } | null>(null);
  const [logError, setLogError] = useState<Error | null>(null);
  useEffect(() => {
    setLimit(run?.maxRounds ?? 3);
  }, [run?.id, run?.blockCode, run?.maxRounds]);
  useEffect(() => {
    setOutput(null);
    setLogError(null);
  }, [sessionId, hostId]);
  const error = action.error ?? logError ?? query.error;
  const canResume = !!run && ["blocked", "stopped"].includes(run.phase);
  const limited = run?.blockCode === "round_limit";
  async function showLog(testId: string, stream: "stdout" | "stderr") {
    try {
      setLogError(null);
      setOutput(
        await getLocalWorkflowTestLog(sessionId, testId, stream, hostId),
      );
    } catch (e) {
      setLogError(e instanceof Error ? e : new Error(String(e)));
    }
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
          <ul className="space-y-2">
            {data?.tests?.map((test) => (
              <li key={test.id} className="rounded border border-border p-2">
                <code>{test.command}</code>
                <p>
                  {t(`localWorkflow.test.${test.status}`)}
                  {test.exitCode != null ? ` (${test.exitCode})` : ""}
                </p>
                <div className="flex gap-3">
                  <button
                    className="underline"
                    onClick={() => void showLog(test.id, "stdout")}
                  >
                    {t("localWorkflow.stdout")}
                  </button>
                  <button
                    className="underline"
                    onClick={() => void showLog(test.id, "stderr")}
                  >
                    {t("localWorkflow.stderr")}
                  </button>
                </div>
              </li>
            ))}
          </ul>
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
