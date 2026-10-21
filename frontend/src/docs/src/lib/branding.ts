// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { PRODUCT } from "../../../shared/product";

// Apply to product prose only; raw source/license attribution is never passed here.
export function visibleProductName(text: string): string {
  return text.replaceAll("Agent Orchestrator", PRODUCT.productName).replace(/\bAO\b/g, PRODUCT.productName);
}
