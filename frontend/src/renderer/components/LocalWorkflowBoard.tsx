import { useQueries } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { localWorkflowQueryOptions } from "../hooks/use-local-workflow";
import type { LocalWorkflowSnapshot } from "../lib/local-workflow-api";
import type { WorkspaceSession } from "../types/workspace";

const stages = [
	"executing",
	"testing",
	"local_review",
	"fixing",
	"awaiting_acceptance",
	"accepted",
] as const;
export function LocalWorkflowBoard({
	sessions,
	hostId,
	renderSessionCard,
}: {
	sessions: WorkspaceSession[];
	hostId?: string;
	renderSessionCard: (
		session: WorkspaceSession,
		details: ReactNode,
	) => ReactNode;
}) {
	const { t } = useTranslation();
	const queries = useQueries({
		queries: sessions.map((s) => localWorkflowQueryOptions(s.id, hostId)),
	});
	return (
		<section
			aria-label={t("localWorkflow.title")}
			className="flex min-h-80 flex-1 flex-col overflow-hidden"
		>
			<h2 className="px-4 py-2 text-xs font-medium">
				{t("localWorkflow.title")}
			</h2>
			<div className="board-horizontal-scrollbar flex-1 overflow-x-auto">
				<div className="grid h-full min-w-[96rem] grid-cols-6 divide-x divide-border-strong">
					{stages.map((stage) => (
						<section
							key={stage}
							aria-label={t("shell.sessionsAria", {
								label: t(`localWorkflow.stage.${stage}`),
							})}
							className="flex min-w-0 flex-col"
							data-column={stage}
						>
							<h3 className="flex h-12 shrink-0 items-center border-b border-border-strong px-4 text-xs font-medium">
								{t(`localWorkflow.stage.${stage}`)}
								<span className="ml-auto text-passive">
									{
										sessions.filter(
											(_, i) =>
												(queries[i].data?.stage ?? "executing") === stage,
										).length
									}
								</span>
							</h3>
							<div className="board-scrollbar min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
								{sessions.map((s, i) => {
									const query = queries[i];
									if ((query.data?.stage ?? "executing") !== stage) return null;
									return (
										<div key={s.id}>
											{renderSessionCard(
												s,
												<div className="space-y-1 px-3 pb-3 text-xs">
													{query.error ? (
														<p role="alert" className="text-destructive">
															{query.error.message}
														</p>
													) : query.isPending ? (
														<p>{t("localWorkflow.loading")}</p>
													) : (
														<LocalWorkflowSummary data={query.data} />
													)}
												</div>,
											)}
										</div>
									);
								})}
							</div>
						</section>
					))}
				</div>
			</div>
		</section>
	);
}

export function LocalWorkflowSummary({
	data,
}: {
	data: LocalWorkflowSnapshot;
}) {
	const { t } = useTranslation();
	const run = data.run;
	if (!run) return <p>{t("localWorkflow.notStarted")}</p>;
	const testsOperation = [...(data.operations ?? [])]
		.reverse()
		.find((op) => op.kind === "tests" && op.cycle === run.cycle);
	const tests = (data.tests ?? []).filter(
		(test) => test.operationId === testsOperation?.id,
	);
	return (
		<>
			<p>
				{t("localWorkflow.cycle", { cycle: run.cycle, limit: run.maxRounds })}
			</p>
			<p>{t(`localWorkflow.phase.${run.phase}`)}</p>
			<p>
				{testsOperation
					? t("localWorkflow.testSummary", {
							passed: tests.filter((test) => test.status === "succeeded")
								.length,
							total: run.testCommands?.length ?? tests.length,
						})
					: t("localWorkflow.testsPending")}
			</p>
			{!!data.requiredFindings?.length && (
				<p>
					{t("localWorkflow.requiredCount", {
						count: data.requiredFindings.length,
					})}
				</p>
			)}
			{data.requiredFindings?.[0] && (
				<p className="line-clamp-2 break-words">
					{data.requiredFindings[0].reason}
				</p>
			)}
			{run.blockMessage && <p className="text-warning">{run.blockMessage}</p>}
			{run.blockCode && (
				<code className="break-all text-warning">{run.blockCode}</code>
			)}
			{run.phase === "ready" && !data.current && (
				<p className="text-warning">{t("localWorkflow.outOfDate")}</p>
			)}
		</>
	);
}
