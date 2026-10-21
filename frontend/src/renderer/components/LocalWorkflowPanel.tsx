// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { aoBridge } from "../lib/bridge";
import { useQuery } from "@tanstack/react-query";
import { LocalWorkflowHistory } from "./LocalWorkflowHistory";
import { LocalWorkflowProviderRetry } from "./LocalWorkflowProviderRetry";
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
  const automationPending = data?.automation?.pending === true;
  const planning =
    run?.automatic &&
    run.phase === "waiting_executor" &&
    ["pending", "running"].includes(run.planState ?? "");
  const plan = run?.verificationPlan;
  const [limit, setLimit] = useState(3);
  const feedbackOwner = JSON.stringify([
    sessionId,
    hostId ?? null,
    run?.id,
    run?.version,
  ]);
  const [feedback, setFeedback] = useState({ owner: "", text: "" });
  const comment = feedback.owner === feedbackOwner ? feedback.text : "";
  const desktop = !hostId && aoBridge.localWorkflow.available;
  const test = [...(data?.operations ?? [])]
    .reverse()
    .find((op) => op.kind === "tests" && op.cycle === run?.cycle);
  const review = [...(data?.operations ?? [])]
    .reverse()
    .find((op) => op.kind === "review" && op.cycle === run?.cycle);
  const canDecide =
    desktop &&
    !automationPending &&
    run?.phase === "ready" &&
    data?.current &&
    !data.requiredFindings.length &&
    test?.state === "succeeded" &&
    review?.state === "succeeded" &&
    review.inputOperationId === test.id &&
    test.snapshotId === review.snapshotId &&
    !!review.reviewRunId;
  const validComment = [...comment].length <= 4000 && !comment.includes("\0");
  function decide(kind: "accept" | "request_changes") {
    if (!canDecide || !run || !test?.snapshotId || !review?.reviewRunId) return;
    action.mutate({
      kind,
      version: run.version,
      workflowId: run.id,
      snapshotId: test.snapshotId,
      testOperationId: test.id,
      reviewRunId: review.reviewRunId,
      comment,
    });
  }

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
  const resumeUncertain = ["provider_execution_uncertain", "provider_uncertain", "review_uncertain"].includes(run?.blockCode ?? "");
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
      {automationPending && (
        <p role="status">{t("localWorkflow.automationPending")}</p>
      )}
      {automationPending && data.automation?.blockMessage && (
        <p role="alert" className="break-words text-warning">
          {data.automation.blockMessage}
        </p>
      )}
      {!query.isLoading && !run && !automationPending && !query.error && (
        <Button
          size="sm"
          disabled={action.isPending}
          onClick={() => action.mutate({ kind: "start" })}
        >
          {t("localWorkflow.start")}
        </Button>
      )}
      {run && !automationPending && (
        <>
          <p role="status">
            {t(
              planning
                ? "localWorkflow.planning"
                : `localWorkflow.phase.${run.phase}`,
            )} ·{" "}
            {t("localWorkflow.cycle", {
              cycle: run.cycle,
              limit: run.maxRounds,
            })}
          </p>
          {plan && (
            <section
              aria-label={t("localWorkflow.verificationPlan")}
              className="space-y-2 rounded border border-border p-2"
            >
              <h4 className="font-medium">
                {t("localWorkflow.verificationPlan")}
              </h4>
              <p>{t(`localWorkflow.profile.${plan.profile}`)}</p>
              <p className="whitespace-pre-wrap break-words">
                {plan.rationale}
              </p>
              {!!plan.commands?.length && (
                <>
                  <h5 className="font-medium">{t("localWorkflow.planCommands")}</h5>
                  <ul className="space-y-1">
                    {plan.commands.map((command, i) => (
                      <li key={i}>
                        <code className="whitespace-pre-wrap break-all">
                          {command}
                        </code>
                      </li>
                    ))}
                  </ul>
                </>
              )}
              {plan.profile === "report" && !run.testCommands?.length && (
                <p>{t("localWorkflow.reportTestsNotApplicable")}</p>
              )}
              {!!plan.checks?.length && (
                <>
                  <h5 className="font-medium">{t("localWorkflow.planChecks")}</h5>
                  <ul className="list-disc space-y-1 pl-4">
                    {plan.checks.map((check, i) => (
                      <li key={i} className="whitespace-pre-wrap break-words">
                        {check}
                      </li>
                    ))}
                  </ul>
                </>
              )}
            </section>
          )}
          <LocalWorkflowProviderRetry retry={run.providerRetry} historical={Boolean(run.closedAt || run.phase === "stopped" || ((run.stopRequested || run.phase === "stopping") && run.providerRetry?.state !== "uncertain"))} />
          {run.blockCode && !(run.blockCode === "provider_waiting" && run.providerRetry) && (
            <code className="break-all text-warning">{run.blockCode}</code>
          )}
          {run.blockMessage && !(run.blockCode === "provider_waiting" && run.providerRetry) && (
            <p className="text-warning">{run.blockMessage}</p>
          )}
          {run.phase === "ready" && (
            <p>
              {t(
                data?.current
                  ? plan?.profile === "report" && !run.testCommands?.length
                    ? "localWorkflow.reportReadyDescription"
                    : "localWorkflow.readyDescription"
                  : "localWorkflow.stale",
              )}
            </p>
          )}
          {run.phase === "ready" && (
            <div className="space-y-2">
              {data?.accepted && <p>{t("localWorkflow.userAccepted")}</p>}
              {!desktop && <p>{t("localWorkflow.desktopRequired")}</p>}
              <label className="block">
                {t("localWorkflow.decisionComment")}
                <textarea
                  aria-label={t("localWorkflow.decisionComment")}
                  className="mt-1 block w-full rounded border border-input bg-background p-2"
                  value={comment}
                  disabled={!canDecide || action.isPending}
                  onChange={(e) =>
                    setFeedback({ owner: feedbackOwner, text: e.target.value })
                  }
                />
              </label>
              <p className="text-muted-foreground">
                {t("localWorkflow.commentLimit")}
              </p>
              <div className="flex flex-wrap gap-2">
                <Button
                  size="sm"
                  disabled={
                    !canDecide ||
                    action.isPending ||
                    !validComment ||
                    data?.accepted
                  }
                  onClick={() => decide("accept")}
                >
                  {t("localWorkflow.accept")}
                </Button>
                <Button
                  size="sm"
                  variant="outline"
                  disabled={
                    !canDecide ||
                    action.isPending ||
                    !validComment ||
                    !comment.trim()
                  }
                  onClick={() => decide("request_changes")}
                >
                  {t("localWorkflow.requestChanges")}
                </Button>
              </div>
            </div>
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
                  resumeUncertain ||
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
        </>
      )}
      {(run || !!data?.previousRuns?.length) && (
        <>
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
