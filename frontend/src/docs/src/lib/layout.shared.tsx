// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import type { BaseLayoutProps } from "fumadocs-ui/layouts/shared";
import { COMPANY } from "./constants";

export function baseOptions(): BaseLayoutProps {
  return {
    nav: {
      url: "/",
      title: (
        <span className="inline-flex items-center gap-2 text-sm font-medium text-foreground">
          <img
            src={"/orchestrator-board.svg"}
            alt=""
            width={20}
            height={20}
            aria-hidden="true"
            className="size-5 shrink-0"
          />
          <span>{COMPANY.NAME} Docs</span>
        </span>
      ),
    },
    themeSwitch: {
      enabled: false,
    },
    links: [
      {
        text: "Project source",
        url: COMPANY.MARKETING_URL,
      },
      {
        text: "GitHub",
        url: COMPANY.GITHUB_URL,
      },
    ],
  };
}
