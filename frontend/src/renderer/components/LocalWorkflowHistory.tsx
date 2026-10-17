import { useTranslation } from "react-i18next";
import type { LocalWorkflowSnapshot } from "../lib/local-workflow-api";

type TestResult = NonNullable<LocalWorkflowSnapshot["tests"]>[number];
export function WorkflowTestResult({
	test,
	onShowLog,
}: {
	test: TestResult;
	onShowLog: (test: TestResult, stream: "stdout" | "stderr") => void;
}) {
	const { t } = useTranslation();
	return (
		<li className="rounded border border-border p-2">
			<code>{test.command}</code>
			<p>
				{t(`localWorkflow.test.${test.status}`)}
				{test.exitCode != null ? ` (${test.exitCode})` : ""}
			</p>
			<div className="flex gap-3">
				{(["stdout", "stderr"] as const).map((stream) => (
					<button
						type="button"
						key={stream}
						className="underline"
						onClick={() => onShowLog(test, stream)}
					>
						{t(`localWorkflow.${stream}`)}
					</button>
				))}
			</div>
		</li>
	);
}
export function LocalWorkflowHistory({
	data,
	onShowLog,
}: {
	data: LocalWorkflowSnapshot;
	onShowLog: (test: TestResult, stream: "stdout" | "stderr") => void;
}) {
	const { t } = useTranslation();
	const operations = data.operations ?? [];
	const cycles = [...new Set(operations.map((op) => op.cycle))].sort(
		(a, b) => a - b,
	);
	return (
		<section aria-label={t("localWorkflow.history")} className="space-y-3">
			<h4 className="font-medium">{t("localWorkflow.history")}</h4>
			{cycles.map((cycle) => (
				<section
					key={cycle}
					aria-label={t("localWorkflow.round", { round: cycle })}
					className="space-y-2 rounded border border-border p-2"
				>
					<h5 className="font-medium">
						{t("localWorkflow.round", { round: cycle })}
					</h5>
					{operations
						.filter((op) => op.cycle === cycle)
						.map((op) => {
							const review = (data.reviews ?? []).find(
								(review) => review.id === op.reviewRunId,
							);
							return (
								<div
									key={op.id}
									className="space-y-2 border-l border-border pl-2"
								>
									<p>
										{t(`localWorkflow.operation.${op.kind}`)} ·{" "}
										{t("localWorkflow.attempt", { attempt: op.attempt })} ·{" "}
										{t(`localWorkflow.operationState.${op.state}`)}
									</p>
									{op.startedAt && (
										<p className="text-muted-foreground">
											<time dateTime={op.startedAt}>
												{new Date(op.startedAt).toLocaleString()}
											</time>
											{op.completedAt && (
												<>
													{" "}
													–{" "}
													<time dateTime={op.completedAt}>
														{new Date(op.completedAt).toLocaleString()}
													</time>
												</>
											)}
										</p>
									)}
									{op.snapshotId && (
										<p>
											{t("localWorkflow.snapshot")}:{" "}
											<code className="break-all">{op.snapshotId}</code>
										</p>
									)}
									{op.errorMessage && (
										<p className="text-warning">{op.errorMessage}</p>
									)}
									{op.errorCode && (
										<code className="text-warning">{op.errorCode}</code>
									)}
									{op.kind === "tests" && (
										<ul className="space-y-2">
											{(data.tests ?? [])
												.filter((test) => test.operationId === op.id)
												.map((test) => (
													<WorkflowTestResult
														key={test.id}
														test={test}
														onShowLog={onShowLog}
													/>
												))}
										</ul>
									)}
									{review && (
										<div className="space-y-2">
											<p>
												{review.harness} ·{" "}
												{review.verdict === "approved" ||
												review.verdict === "changes_requested"
													? t(`localWorkflow.verdict.${review.verdict}`)
													: t("localWorkflow.verdict.unknown")}
											</p>
											{review.localResult && (
												<>
													<p className="whitespace-pre-wrap">
														{review.localResult.summary}
													</p>
													<ul className="space-y-2">
														{review.localResult.findings.map((finding, i) => (
															<li key={i}>
																<p
																	className={
																		finding.kind === "required"
																			? "text-warning"
																			: "text-muted-foreground"
																	}
																>
																	{t(
																		finding.kind === "required"
																			? "localWorkflow.required"
																			: "localWorkflow.suggestion",
																	)}{" "}
																	·{" "}
																	<code>
																		{finding.path}:{finding.line}
																	</code>
																</p>
																<p className="whitespace-pre-wrap">
																	{finding.reason}
																</p>
															</li>
														))}
													</ul>
												</>
											)}
										</div>
									)}
									{op.kind === "review" &&
										!review &&
										!["reserved", "running"].includes(op.state) && (
											<p>{t("localWorkflow.reviewUnavailable")}</p>
										)}
								</div>
							);
						})}
				</section>
			))}
			<ul className="space-y-2">
				{(data.tests ?? [])
					.filter(
						(test) => !operations.some((op) => op.id === test.operationId),
					)
					.map((test) => (
						<WorkflowTestResult
							key={test.id}
							test={test}
							onShowLog={onShowLog}
						/>
					))}
			</ul>
		</section>
	);
}
