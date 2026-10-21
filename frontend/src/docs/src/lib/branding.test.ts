// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { expect, it } from "vitest";
import { visibleProductName } from "./branding";
it("uses fork branding without rewriting upstream attribution", () => {
 expect(visibleProductName("Agent Orchestrator and AO desktop")).toBe("Orchestrator Board and Orchestrator Board desktop");
});
