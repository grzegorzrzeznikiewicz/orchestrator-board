# Orchestrator Board artwork and notices
<!-- Modified for Orchestrator Board by Gama Software, 2026 (OB-1). -->


The project owner supplied and authorized adaptation of Gama Software artwork on 2026-10-17 for OB-10. Source repositories were read only. The new editable mark retains the company logo's three blue diamonds and pairs them with a three-column workflow board. It follows the supplied db-anonymizer vector adaptation pattern; it does not reuse the upstream AO mascot as the product mark.

| Source supplied by the owner | SHA-256 |
| --- | --- |
| company-site: theme/gama-software/assets/images/gama-software-logo.png | `f47de0880b5526c7c82f1f4355574b8396d071a34e9c127a362944066b287786` |
| db-anonymizer: packaging/icons/db-anonymizer.svg | `bc6c4b6fb5e2cfeea51ee631be76b14e0937b5d680aa5db896b26d0e646286e4` |

The source paths are recorded in the OB-10 design spec. Gama Software's name and artwork remain the owner's marks. This provenance is not a grant to unrelated third-party marks.

`frontend/assets/orchestrator-board.svg` is the editable product master, and `frontend/assets/tray.svg` is the monochrome menu-bar master. The SVG copies at `frontend/assets/ao-logo.svg`, `frontend/src/renderer/assets/ao-logo.svg`, `assets/ao-logo.svg`, and `frontend/src/docs/public/orchestrator-board.svg` contain the same new mark. Existing `ao-logo` filenames and `AOMascot` component export are internal compatibility names; the displayed artwork is the Gama board mark. The unused original mascot remains in source history/assets rather than being misattributed.

The master SVG was rasterized with the bundled Sharp runtime, without editing the supplied bitmap or changing source repositories. Derived assets: `frontend/assets/icon.png` and renderer `ao-logo.png` at 1024×1024; macOS `icon.icns` from an iconset containing 16/32/128/256/512 point sizes and their 2× variants; `icon.ico` with 16/24/32/48/64/128/256 PNG entries; transparent black tray templates at 16 and 32 pixels. The monochrome tray geometry deliberately omits small workflow card detail. Regenerate from the editable SVGs with a vector rasterizer, then assemble ICNS using macOS `iconutil`; do not rescale a small bitmap into a master.

## Distributed attribution

The root `LICENSE` remains byte-identical to upstream Apache-2.0, including Copyright 2026 Untrivial. The inherited desktop frontend package metadata said MIT; the fork's distributed desktop package now declares Apache-2.0 to match the root. Dependency declarations are retained, including inherited local product-ui metadata, rather than being silently relicensed.

`frontend/scripts/generate-license-resources.mjs` generates `frontend/resources/licenses/` offline after runtime preparation. It uses installed production lockfile entries, bundled ACP adapters/Node, actual daemon Go build dependencies for the target platform, the Go standard-library license, available bundled tmux notices, and agent asset notices. It excludes development packages and fails for a missing required production package or missing license. Optional platform packages absent from the build are omitted. `OB_LICENSE_GO` can select the pinned Go executable; normal builds use Go from PATH.

About is available from the native macOS app menu and renderer Help menu. Its Licenses action opens the full bundled LICENSE, NOTICE, THIRD-PARTY-LICENSES.txt, ASSET-NOTICES.txt, Electron-LICENSE.txt or Chromium-LICENSES.html in the system viewer. Packaging includes this directory before signing. Exact package-byte verification belongs to the integration report.

The collector retains supplied standalone legal and attribution documents recursively, including nested `LICENSES`, vendored licenses, `ThirdPartyNotice`, `CopyrightNotice` and `AUTHORS`. It also retains actual legal comment paragraphs from sources and source-map `sourcesContent`, plus explicit README license/copyright sections and standalone copyright footers outside code examples. JavaScript/TypeScript comments use the existing build-time TypeScript parser so regex and string literals are not misattributed. Original text, including CRLF, is preserved. This conservative source inventory is not a claim that every inventoried dependency or source-map fragment is compiled into the application.

The final raw-byte audit covers all 709 installed production/optional package records, 713 standalone notices/attributions and 83 previously omitted original comment blocks. The generator has regression coverage for the actual omissions and rejects a retained snapshot whose `packageVersion` does not match the installed package. Source URLs, version evidence, explicit limitations and SHA-256 values are in `frontend/scripts/license-overrides/sources.json`. The final package must contain these generated resources exactly.

Some exact npm archives contain no full original copyright/license file. Four MIT-declared packages retain standard MIT terms and their published manifest attribution: `@apm-js-collab/code-transformer-bundler-plugins@0.7.4`, `get-folder-size@2.0.1`, `tiny-each-async@2.0.3` and `tmp-promise@3.0.3`. Their records explicitly say those terms are not a recovered original copyright notice; no holder or date is invented. For `lazy-val@1.0.5`, the retained full MIT notice is recovered from the original electron-builder implementation, Copyright (c) 2015 Loopline Systems. The immutable source and extraction chain are recorded separately from the later package author and modifications; no standalone 1.0.5 tarball notice is claimed.

The supplemental `react-remove-scroll-bar@2.3.8` repository text remains labeled with its historical version limitation. Research links its implementation to the preceding source and later published notice, but does not claim to recover an original 2.3.8 archive notice. `@posthog/core` has a registry-delivered source attestation/content bridge; its attestation signature was not independently verified. `@sentry/server-utils` has an exact release-tag/source-content bridge. qrcode-generator and standardwebhooks use npm's exact gitHead. PostHog and standardwebhooks declare MIT in npm while their corresponding repository text says Apache-2.0; both published statements are retained without silently relicensing them.

These documented upstream provenance limitations are distinct from dropping supplied notices, which the technical audit checks and corrects. The audit is not a certificate of complete legal compliance.

## Compatibility references

The `ao` daemon/CLI, API identifiers, ~/.ao storage, AO_DATA_DIR variables, original mobile app/store names, old-install migration descriptions and vendor AO Cloud services keep their names. Provider authentication, API, billing and mobile service endpoints retain their real vendor addresses. The own desktop's bundle/name/executable/protocol come from `frontend/product.json`.

The upstream hosted auth and billing bounce pages emit `ao-app://`. This fork registers/parses only `orchestrator-board://` and defaults WorkOS sign-in to the existing loopback callback at 127.0.0.1:3000. An occupied callback port fails before opening a browser. An explicit AO_CLOUD_AUTH_REDIRECT requires an allowlisted loopback URL or a fork-aware HTTPS bounce service; hosted billing/auth return compatibility has not been externally certified. No hosted vendor service was changed.

The fork's packaged Chromium profile defaults to `~/.ao/orchestrator-board/electron`. An explicit nonblank `AO_DATA_DIR` selects `<resolved-data-dir>/orchestrator-board/electron`, including isolated native tests. The default development profile is `~/.ao/dev/orchestrator-board/electron`; `AO_DEV_ELECTRON_DIR` remains an advanced explicit override. Original `~/.ao/electron` and `~/.ao/dev/electron` profiles are not opened, copied or migrated by these defaults. See [data and coexistence](ob-10-fork-identity.md#data-and-coexistence) for the saved Finder/Dock launch selection.
