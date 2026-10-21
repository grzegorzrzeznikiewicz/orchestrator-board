// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { useTranslation } from "react-i18next";
import ReactMarkdown from "react-markdown";
import type { ReviewRunFacts } from "../lib/session-reviews";
import { formatTimeCompact } from "../lib/format-time";

export function LocalReviewHistory({ runs }: { runs: ReviewRunFacts[] }) {
	const { t } = useTranslation();
	const localRuns = runs
		.filter((run) => run.targetKind === "local")
		.sort((a, b) => (b.round ?? 0) - (a.round ?? 0) || b.createdAt.localeCompare(a.createdAt));
	return (
		<section className="space-y-3" aria-label={t("inspector.localReview")}>
			{localRuns.length === 0 && (
				<p className="text-xs text-muted-foreground">{t("inspector.review.notRun")}</p>
			)}
			{localRuns.map((run, index) => {
				const status = run.status === "failed" ? t("inspector.review.failed")
					: !run.current || run.invalidatedAt
					? t("inspector.localReviewStale")
					: run.status === "running" ? t("inspector.review.reviewing")
					: run.status === "cancelled" ? t("inspector.review.cancelled")
					: run.verdict === "approved" ? t("inspector.localReviewApproved")
					: run.verdict === "changes_requested" ? t("inspector.review.changesRequested")
					: t("inspector.review.notRun");
				return (
					<details key={run.id} open={index === 0} className="rounded-md border border-border p-3 text-xs">
						<summary className="cursor-pointer space-x-2">
							<span>{run.harness}</span>
							{run.round != null && run.round > 0 && <span>{t("inspector.localReviewRound", {round: run.round})}</span>}
							<span>{formatTimeCompact(run.createdAt)}</span>
							<span>{status}</span>
						</summary>
						<p className="my-2 break-all font-mono text-2xs">
							{t("inspector.localReviewSnapshot")}: {run.snapshotId}
						</p>
						{run.invalidationReason && (
							<p role="status" className="mb-2 text-warning">{run.invalidationReason}</p>
						)}
						{run.localResult && (
							<div className="space-y-2">
								<div className="prose prose-sm dark:prose-invert"><ReactMarkdown>{run.localResult.summary}</ReactMarkdown></div>
								<ul className="space-y-2">
									{run.localResult.findings.map((finding, index) => (
										<li key={index} className="rounded border border-border p-2">
											<span className={finding.kind === "required" ? "text-warning" : "text-muted-foreground"}>{t(finding.kind === "required" ? "inspector.localReviewRequired" : "inspector.localReviewSuggestion")}</span>
											<code className="ml-2 break-all">{finding.path}:{finding.line}</code>
											<p className="mt-1 whitespace-pre-wrap">{finding.reason}</p>
										</li>
									))}
								</ul>
							</div>
						)}
						{!run.localResult && run.body && (
							<div className="prose prose-sm dark:prose-invert"><ReactMarkdown>{run.body}</ReactMarkdown></div>
						)}
					</details>
				);
			})}
		</section>
	);
}
