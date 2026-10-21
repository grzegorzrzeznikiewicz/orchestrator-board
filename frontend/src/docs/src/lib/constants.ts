// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { PRODUCT } from "../../../shared/product";
export const COMPANY = {
  NAME: PRODUCT.productName,
  MARKETING_URL: PRODUCT.sourceUrl,
  GITHUB_URL: PRODUCT.sourceUrl,
} as const;
