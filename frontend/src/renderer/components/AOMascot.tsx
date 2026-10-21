// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import brandMark from "../assets/ao-logo.svg";

/** Kept under the existing component API; artwork is the editable Gama board mark. */
export function AOMascot({ className }: { className: string }) {
 return <img className={`object-contain ${className}`} src={brandMark} alt="" aria-hidden="true" />;
}
