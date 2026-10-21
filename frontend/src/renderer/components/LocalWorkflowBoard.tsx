// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { useTranslation } from "react-i18next";
import { localWorkflowSummaryOptions } from "../hooks/use-local-workflow";
import type { LocalWorkflowCard } from "../lib/local-workflow-api";
import type { WorkspaceSession } from "../types/workspace";
import { LocalWorkflowProviderRetry } from "./LocalWorkflowProviderRetry";

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
	const query = useQuery(localWorkflowSummaryOptions(sessions.map((s) => s.id), hostId));
	const cards = new Map(query.data?.map((card) => [card.sessionId, card]));
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
											(session) =>
												(cards.get(session.id)?.stage ?? "executing") === stage,
										).length
									}
								</span>
							</h3>
							<div className="board-scrollbar min-h-0 flex-1 space-y-3 overflow-y-auto p-3">
								{sessions.map((s) => {
									const card = cards.get(s.id);
									if ((card?.stage ?? "executing") !== stage) return null;
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
														<LocalWorkflowSummary data={card} />
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
	data?: LocalWorkflowCard;
}) {
	const { t } = useTranslation();
	const run = data?.run;
	if (data?.automation?.pending)
		return (
			<>
				<p>{t("localWorkflow.automationPending")}</p>
				{data.automation.blockMessage && (
					<p role="alert" className="break-words text-warning">
						{data.automation.blockMessage}
					</p>
				)}
			</>
		);
	if (!data || !run) return <p>{t("localWorkflow.notStarted")}</p>;
	const planning =
		run.automatic &&
		run.phase === "waiting_executor" &&
		["pending", "running"].includes(run.planState ?? "");
	const profile =
		run.profile === "implementation" || run.profile === "report"
			? run.profile
			: undefined;
	return (
		<>
			<p>
				{t("localWorkflow.cycle", { cycle: run.cycle, limit: run.maxRounds })}
			</p>
			<p>
				{t(data.accepted ? "localWorkflow.stage.accepted" : planning ? "localWorkflow.planning" : `localWorkflow.phase.${run.phase}`)}
			</p>
			{profile && <p>{t(`localWorkflow.profile.${profile}`)}</p>}
			<p>
				{profile === "report" && !data.testsTotal
					? t("localWorkflow.reportTestsNotApplicable")
					: data.testsPresent
					? t("localWorkflow.testSummary", {
							passed: data.testsPassed,
							total: data.testsTotal,
						})
					: t("localWorkflow.testsPending")}
			</p>
			{!!data.requiredCount && (
				<p>
					{t("localWorkflow.requiredCount", {
						count: data.requiredCount,
					})}
				</p>
			)}
			{data.requiredReason && (
				<p className="line-clamp-2 break-words">
					{data.requiredReason}
				</p>
			)}
			{data.testsQueued && <p>{t("localWorkflow.operationState.reserved")}</p>}
			<LocalWorkflowProviderRetry retry={run.providerRetry} historical={run.phase === "stopped" || (run.phase === "stopping" && run.providerRetry?.state !== "uncertain")} />
			{run.blockMessage && !(run.blockCode === "provider_waiting" && run.providerRetry) && <p className="text-warning">{run.blockMessage}</p>}
			{run.blockCode && !(run.blockCode === "provider_waiting" && run.providerRetry) && (
				<code className="break-all text-warning">{run.blockCode}</code>
			)}
			{run.phase === "ready" && !data.current && (
				<p className="text-warning">{t("localWorkflow.outOfDate")}</p>
			)}
		</>
	);
}
