// Modified for Orchestrator Board by Gama Software, 2026.

import { useTranslation } from "react-i18next";
import type { LocalWorkflowSnapshot } from "../lib/local-workflow-api";

export function LocalWorkflowVerificationContract({ contract }: {
  contract: NonNullable<LocalWorkflowSnapshot["run"]>["verificationContract"];
}) {
  const { t } = useTranslation();
  if (!contract) return null;
  return (
    <section aria-label={t("localWorkflow.verificationScope")} className="space-y-2 rounded border border-border p-2">
      <h4 className="font-medium">{t("localWorkflow.verificationScope")}</h4>
      <code className="break-all">{contract.scopePath}</code>
      <p className="whitespace-pre-wrap break-words">{contract.basis}</p>
      <p className="text-muted-foreground">{t("localWorkflow.verificationScopeDescription")}</p>
      <ul className="space-y-1">
        {contract.commands.map((command) => <li key={command}><code className="whitespace-pre-wrap break-all">{command}</code></li>)}
      </ul>
    </section>
  );
}
