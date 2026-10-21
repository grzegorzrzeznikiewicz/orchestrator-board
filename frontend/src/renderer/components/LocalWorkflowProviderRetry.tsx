// Modified for Orchestrator Board by Gama Software, 2026 (OB-9).
import { useTranslation } from "react-i18next";
import type { LocalWorkflowProviderRetryFacts } from "../lib/local-workflow-api";

// Retry scheduling and access checks belong to the daemon. This renders its facts.
export function LocalWorkflowProviderRetry({ retry, historical = false }: {
	retry?: LocalWorkflowProviderRetryFacts;
	historical?: boolean;
}) {
	const { t, i18n } = useTranslation();
	if (!retry) return null;
	const state = retry.state === "waiting" || retry.state === "checking" || retry.state === "recovered" || retry.state === "exhausted" || retry.state === "uncertain" ? retry.state : "uncertain";
	const reason = retry.reason === "authentication" || retry.reason === "rate_limited" || retry.reason === "unavailable" ? t(`localWorkflow.providerRetry.reason.${retry.reason}`) : undefined;
	const priorIssue = historical || state === "recovered";
	const timestamp = (value: string | null | undefined, label: "failureAt" | "lastAt" | "nextAt") => {
		if (!value) return null;
		const date = new Date(value);
		if (!Number.isFinite(date.getTime())) return null;
		return <p><time dateTime={value}>{t(`localWorkflow.providerRetry.${label}`, { time: date.toLocaleString(i18n.resolvedLanguage) })}</time></p>;
	};
	return (
		<section aria-label={t(historical ? "localWorkflow.providerRetry.history" : "localWorkflow.providerRetry.title")} className="space-y-1 break-words">
			{historical && <p className="font-medium">{t("localWorkflow.providerRetry.history")}</p>}
			<p>{t(`localWorkflow.providerRetry.state.${state}`)}</p>
			{!historical && state === "exhausted" && <p>{t("localWorkflow.providerRetry.manualAttention")}</p>}
			{!historical && state === "uncertain" && <p>{t("localWorkflow.providerRetry.uncertaintyAttention")}</p>}
			{retry.stage === "planner" && <p>{t("localWorkflow.planning")}</p>}
			{retry.stage === "review" && <p>{t("localWorkflow.operation.review")}</p>}
			{reason && <p>{priorIssue ? t("localWorkflow.providerRetry.previousReason", { reason }) : reason}</p>}
			<p>{t("localWorkflow.providerRetry.attempts", { attempt: retry.attempt, max: retry.maxAttempts })}</p>
			{timestamp(retry.failureAt, "failureAt")}
			{timestamp(retry.lastAt, "lastAt")}
			{!historical && state === "waiting" && timestamp(retry.nextAt, "nextAt")}
		</section>
	);
}
