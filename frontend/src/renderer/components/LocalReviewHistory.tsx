import { useTranslation } from "react-i18next";
import ReactMarkdown from "react-markdown";
import type { ReviewRunFacts } from "../lib/session-reviews";
import { formatTimeCompact } from "../lib/format-time";

export function LocalReviewHistory({ runs }: { runs: ReviewRunFacts[] }) {
	const { t } = useTranslation();
	const localRuns = runs
		.filter((run) => run.targetKind === "local")
		.sort((a, b) => b.createdAt.localeCompare(a.createdAt));
	return (
		<section className="space-y-3" aria-label={t("inspector.localReview")}>
			{localRuns.length === 0 && (
				<p className="text-xs text-muted-foreground">{t("inspector.review.notRun")}</p>
			)}
			{localRuns.map((run, index) => {
				const status = !run.current || run.invalidatedAt
					? t("inspector.localReviewStale")
					: run.status === "running" ? t("inspector.review.reviewing")
					: run.status === "failed" ? t("inspector.review.failed")
					: run.status === "cancelled" ? t("inspector.review.cancelled")
					: run.verdict === "approved" ? t("inspector.localReviewApproved")
					: run.verdict === "changes_requested" ? t("inspector.review.changesRequested")
					: t("inspector.review.notRun");
				return (
					<details key={run.id} open={index === 0} className="rounded-md border border-border p-3 text-xs">
						<summary className="cursor-pointer space-x-2">
							<span>{run.harness}</span>
							<span>{formatTimeCompact(run.createdAt)}</span>
							<span>{status}</span>
						</summary>
						<p className="my-2 break-all font-mono text-2xs">
							{t("inspector.localReviewSnapshot")}: {run.snapshotId}
						</p>
						{run.invalidationReason && (
							<p role="status" className="mb-2 text-warning">{run.invalidationReason}</p>
						)}
						{run.body && (
							<div className="prose prose-sm dark:prose-invert"><ReactMarkdown>{run.body}</ReactMarkdown></div>
						)}
					</details>
				);
			})}
		</section>
	);
}
