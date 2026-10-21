// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

// Modified for Orchestrator Board by Gama Software, 2026 (OB-10).

import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import { aoBridge } from "../../lib/bridge";
import { TooltipProvider } from "../ui/tooltip";
import { ReportProblemContent } from "./ReportProblemContent";

vi.mock("../../lib/telemetry", async (importOriginal) => ({ ...await importOriginal<typeof import("../../lib/telemetry")>(), captureRendererEvent: vi.fn() }));
afterEach(() => vi.restoreAllMocks());

it("offers the configured fork issue action without upstream Discord or email contacts", async () => {
 const openExternal = vi.spyOn(aoBridge.app, "openExternal").mockResolvedValue(undefined);
 const clipboard = vi.spyOn(aoBridge.clipboard, "writeText").mockResolvedValue(undefined);
 render(<TooltipProvider><ReportProblemContent active /></TooltipProvider>);
 const submit = screen.getByRole("button", { name: "Copy & Create GitHub Issue" });
 expect(submit).toBeDisabled();
 expect(screen.queryByRole("button", { name: "Copy & Open Discord" })).not.toBeInTheDocument();
 expect(screen.queryByRole("button", { name: "Copy & Open Email" })).not.toBeInTheDocument();
 fireEvent.change(screen.getByRole("textbox", { name: "Title" }), { target: { value: "Fork report" } });
 fireEvent.change(screen.getByRole("textbox", { name: "What happened?" }), { target: { value: "Reproduction details" } });
 expect(submit).toBeEnabled();
 fireEvent.click(submit);
 await waitFor(() => expect(openExternal).toHaveBeenCalledTimes(1));
 const destination = new URL(openExternal.mock.calls[0][0]);
 expect(`${destination.origin}${destination.pathname}`).toBe("https://github.com/grzegorzrzeznikiewicz/orchestrator-board/issues/new");
 expect(destination.searchParams.get("title")).toBe("Fork report");
 expect(destination.searchParams.get("body")).toContain("Reproduction details");
 expect(clipboard).toHaveBeenCalledWith(expect.stringContaining("Orchestrator Board version:"));
 expect(clipboard.mock.calls[0][0]).not.toMatch(/untrivial\.ai|discord\.gg/);
});
