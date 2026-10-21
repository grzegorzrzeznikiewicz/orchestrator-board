// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { mkdtempSync, mkdirSync, writeFileSync, rmSync } from "node:fs";
import os from "node:os";
import path from "node:path";
import { expect, it } from "vitest";
import { collectNpmNotices, legalTexts, embeddedLegalTexts, bundledRuntimeNotices } from "./generate-license-resources.mjs";
it("retains full production notices, excludes development packages and fails when a license is missing", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-test-"));
 try {
  mkdirSync(path.join(root, "node_modules/runtime"), { recursive: true });
  writeFileSync(path.join(root, "package-lock.json"), JSON.stringify({ packages: { "node_modules/runtime": { version: "1.0.0" }, "node_modules/dev": { dev: true } } }));
  writeFileSync(path.join(root, "node_modules/runtime/package.json"), JSON.stringify({ name: "runtime", version: "1.0.0", license: "MIT" }));
  expect(() => collectNpmNotices(root)).toThrow("Missing full license text");
  writeFileSync(path.join(root, "node_modules/runtime/LICENSE"), "Copyright Runtime Author\nFull operative license terms\n");
  const notices = collectNpmNotices(root);
  expect(notices).toHaveLength(1);
  expect(notices[0]).toContain("Copyright Runtime Author\nFull operative license terms");
  expect(notices[0]).not.toContain("node_modules/dev");
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it("does not silently omit a required installed production dependency", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-missing-"));
 try {
  writeFileSync(path.join(root, "package-lock.json"), JSON.stringify({ packages: { "node_modules/required": { version: "1.0.0" } } }));
  expect(() => collectNpmNotices(root)).toThrow("Required production package is not installed");
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it("retains every full notice in a bundled runtime licenses directory", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-runtime-"));
 try {
  mkdirSync(path.join(root, "licenses"));
  writeFileSync(path.join(root, "licenses/tmux.txt"), "Original tmux notice");
  expect(legalTexts(root)).toEqual([{ name: "licenses/tmux.txt", text: "Original tmux notice" }]);
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it.each([
 ["LICENSES/shimmer/LICENSE", "BSD 2-Clause License\r\nCopyright Original Shimmer Holder\r\nComplete original BSD terms\r\n"],
 ["src/internal/qs/LICENSE.md", "BSD 3-Clause License\nCopyright Original Query Parser Holder\nComplete original BSD terms\n"],
 ["src/license_header", "/*! @license Original Sanitizer VERSION | (c) Original Contributors */\n"],
 ["ThirdPartyNoticeText.txt", "Original embedded dependency notice\nCopyright Original Embedded Holder\nComplete original terms\n"],
 ["AUTHORS.md", "# Original Authors\nFirst Original Contributor\nSecond Original Contributor\n"],
 ["CopyrightNotice.txt", "Copyright Original Runtime Holder\r\nComplete original MIT terms\r\n"],
])("retains the exact supplied nested notice at %s", (relative, original) => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-nested-"));
 try {
  const file = path.join(root, relative);
  mkdirSync(path.dirname(file), { recursive: true });
  writeFileSync(file, original);
  expect(legalTexts(root)).toEqual([{ name: relative, text: original }]);
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it("does not mistake an icon source or source map named copyright for a standalone notice", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-icon-"));
 try {
  mkdirSync(path.join(root, "dist/icons"), { recursive: true });
  writeFileSync(path.join(root, "dist/icons/copyright.mjs"), "/* @license Icon Library */\nexport const copyright = [];\n");
  writeFileSync(path.join(root, "dist/icons/copyright.mjs.map"), '{"version":3,"sourcesContent":["icon source"]}');
  expect(legalTexts(root)).toEqual([]);
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it("rejects a retained license snapshot for a different package version", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-version-"));
 try {
  mkdirSync(path.join(root, "node_modules/runtime"), { recursive: true });
  writeFileSync(path.join(root, "package-lock.json"), JSON.stringify({ packages: { "node_modules/runtime": { version: "2.0.0" } } }));
  writeFileSync(path.join(root, "node_modules/runtime/package.json"), JSON.stringify({ name: "runtime", version: "2.0.0", license: "MIT" }));
  const overrides = { runtime: [{ name: "retained original MIT notice", text: "Original notice and full MIT terms", packageVersion: "1.0.0" }] };
  expect(() => collectNpmNotices(root, overrides)).toThrow("License snapshot version mismatch: runtime@2.0.0");
  overrides.runtime[0].packageVersion = "2.0.0";
  expect(collectNpmNotices(root, overrides).join("\n")).toContain("Original notice and full MIT terms");
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it("can reuse the same versioned overrides after appending a package README notice", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-override-reuse-"));
 try {
  const directory = path.join(root, "node_modules/runtime");
  mkdirSync(directory, { recursive: true });
  writeFileSync(path.join(root, "package-lock.json"), JSON.stringify({ packages: { "node_modules/runtime": { version: "1.0.0" } } }));
  writeFileSync(path.join(directory, "package.json"), JSON.stringify({ name: "runtime", version: "1.0.0", license: "MIT" }));
  writeFileSync(path.join(directory, "README.md"), "## License\nCopyright Original README Holder\n");
  const overrides = { runtime: [{ name: "retained original MIT notice", text: "Original full MIT terms", packageVersion: "1.0.0" }] };
  const first = collectNpmNotices(root, overrides);
  expect(first.join("\n")).toContain("Original full MIT terms");
  expect(first.join("\n")).toContain("Copyright Original README Holder");
  expect(collectNpmNotices(root, overrides)).toEqual(first);
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it.each([
 "## License\r\nCopyright (c) 2023 Original README Holder\r\nReleased under the MIT license.\r\n\r\n",
 "Copyright and License\n---------------------\nCopyright (c) 2023 Original README Holder\nReleased under the MIT license.\n\n",
 "## Authors\nFirst Original README Contributor\nSecond Original README Contributor\n\n",
 "© 2024–present. MIT License.\n",
])("preserves a published README legal section beside the standalone license", (section) => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-readme-"));
 try {
  const directory = path.join(root, "node_modules/runtime");
  mkdirSync(directory, { recursive: true });
  writeFileSync(path.join(root, "package-lock.json"), JSON.stringify({ packages: { "node_modules/runtime": { version: "1.0.0" } } }));
  writeFileSync(path.join(directory, "package.json"), JSON.stringify({ name: "runtime", version: "1.0.0", license: "MIT" }));
  writeFileSync(path.join(directory, "LICENSE"), "Copyright (c) 2019 Original Standalone Holder\nFull MIT terms\n");
  writeFileSync(path.join(directory, "README.md"), `# Usage\nUNRELATED_README_CODE\n\n\`\`\`md\n## License\nCopyright FAKE_FENCED_NOTICE\n\`\`\`\n\n${section}# Support\nUNRELATED_SUPPORT_TEXT\n`);
  const notices = collectNpmNotices(root).join("\n");
  expect(notices).toContain("Copyright (c) 2019 Original Standalone Holder\nFull MIT terms\n");
  expect(notices).toContain(section);
  expect(notices).not.toContain("FAKE_FENCED_NOTICE");
  expect(notices).not.toContain("UNRELATED_README_CODE");
  expect(notices).not.toContain("UNRELATED_SUPPORT_TEXT");
 } finally { rmSync(root, { recursive: true, force: true }); }
});


it("preserves nested embedded MIT and SPDX notices beside a different package license", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-embedded-"));
 try {
  const directory = path.join(root, "node_modules/runtime");
  mkdirSync(path.join(directory, "src/vendor"), { recursive: true });
  mkdirSync(path.join(directory, "node_modules/child"), { recursive: true });
  writeFileSync(path.join(root, "package-lock.json"), JSON.stringify({ packages: { "node_modules/runtime": { version: "1.0.0" } } }));
  writeFileSync(path.join(directory, "package.json"), JSON.stringify({ name: "runtime", version: "1.0.0", license: "BSD-3-Clause" }));
  writeFileSync(path.join(directory, "LICENSE"), "BSD root holder");
  const embedded = "/* Copyright (c) 2019 Distinct Embedded Holder\nPermission is hereby granted to retain these full MIT terms.\nTHE SOFTWARE IS PROVIDED AS IS. */";
  const lineNotice = "// SPDX-License-Identifier: Apache-2.0\n// Copyright 2021 Separate Line Holder";
  writeFileSync(path.join(directory, "src/parser.ts"), `${embedded}\nexport const parse = 1;\n${lineNotice}\n`);
  writeFileSync(path.join(directory, "src/quoted.ts"), 'export const text = "/* Copyright FAKE_QUOTED_NOTICE */";');
  writeFileSync(path.join(directory, "src/ignored.map"), "/* Copyright IGNORED_MAP_NOTICE */");
  writeFileSync(path.join(directory, "node_modules/child/index.js"), "/* Copyright CHILD_DUPLICATE_NOTICE */");
  writeFileSync(path.join(directory, "src/vendor/uuid.ts"), "/* @license Apache-2.0\n * @copyright 2021 Vendored Holder */");
  const notices = collectNpmNotices(root).join("\n");
  expect(notices).toContain("BSD root holder");
  expect(notices).toContain(embedded);
  expect(notices).toContain(lineNotice);
  expect(notices).toContain("@copyright 2021 Vendored Holder");
  expect(notices).toContain("src/parser.ts");
  expect(notices).not.toContain("FAKE_QUOTED_NOTICE");
  expect(notices).not.toContain("IGNORED_MAP_NOTICE");
  expect(notices).not.toContain("CHILD_DUPLICATE_NOTICE");
 } finally { rmSync(root, { recursive: true, force: true }); }
});


it("retains an exact Go production-file block without scanning uncompiled module files", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-go-"));
 try {
  const source = path.join(root, "int128.go");
  const notice = "/*\nThe MIT License (MIT)\nCopyright (c) 2019 Distinct Go Holder\nPermission is hereby granted to use these full terms.\n*/";
  writeFileSync(source, `${notice}\npackage runtime`);
  writeFileSync(path.join(root, "unused.go"), "/* Copyright UNCOMPILED_HOLDER */");
  const notices = embeddedLegalTexts(root, [source]);
  expect(notices).toEqual([{ name: "embedded source notice: int128.go:1", text: notice }]);
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it("retains only original comments from valid source-map sourcesContent with their map/source origin", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-sourcemap-"));
 try {
  const notice = "/* Copyright Original Vendored Holder\r\n * SPDX-License-Identifier: BSD-2-Clause\r\n */";
  writeFileSync(path.join(root, "bundle.js.map"), JSON.stringify({ version: 3, sources: ["../../vendor/source.ts", "unused.ts"], sourcesContent: [`${notice}\nexport const UNRELATED_ORIGINAL_CODE = 1;\nconst quoted = "/* Copyright FAKE_MAP_STRING */";`, null] }));
  writeFileSync(path.join(root, "invalid.map"), "/* Copyright FAKE_NON_JSON_MAP */");
  const notices = embeddedLegalTexts(root);
  expect(notices).toHaveLength(1);
  expect(notices[0].name).toContain("bundle.js.map");
  expect(notices[0].name).toContain("../../vendor/source.ts:1");
  expect(notices[0].text).toBe(notice);
  expect(JSON.stringify(notices)).not.toContain("UNRELATED_ORIGINAL_CODE");
  expect(JSON.stringify(notices)).not.toContain("FAKE_MAP_STRING");
  expect(JSON.stringify(notices)).not.toContain("FAKE_NON_JSON_MAP");
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it("preserves supplied vendored helper notices even when the published file is a test", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-helper-"));
 try {
  const notice = "/* Copyright 2011 Original Helper Holder\n * Complete original BSD terms.\n */";
  writeFileSync(path.join(root, "uri-js.test.js"), `${notice}\nconst publishedTestHelper = true;`);
  expect(embeddedLegalTexts(root)).toEqual([{ name: "embedded source notice: uri-js.test.js:1", text: notice }]);
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it("preserves a real bundled notice after a regular expression containing quotes", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-regexp-"));
 try {
  const notice = "/*! Bundled license information:\n * Copyright Original Bundled Holder\n * Complete original MIT terms.\n */";
  writeFileSync(path.join(root, "bundle.js"), `const quote = /"/;\n${notice}\n`);
  expect(embeddedLegalTexts(root)).toEqual([{ name: "embedded source notice: bundle.js:2", text: notice }]);
 } finally { rmSync(root, { recursive: true, force: true }); }
});

it("does not classify a source comment listing entity types as a copyright notice", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-entity-label-"));
 try {
  writeFileSync(path.join(root, "entities.js"), "// Copyright, trademark, and registration\nexport const entities = { copy: 169 };\n");
  expect(embeddedLegalTexts(root)).toEqual([]);
 } finally { rmSync(root, { recursive: true, force: true }); }
});


it("includes all supplied browser runtime license bytes without reading its binary", () => {
 const root = mkdtempSync(path.join(os.tmpdir(), "ob-license-browser-"));
 try {
  mkdirSync(path.join(root, "agent-browser"));
  for (const name of ["LICENSE-agent-browser", "LICENSE-axe-core", "LICENSE-axe-core-THIRD-PARTY"]) {
   writeFileSync(path.join(root, "agent-browser", name), `Exact supplied ${name}\r\nOriginal copyright and terms\r\n`);
  }
  writeFileSync(path.join(root, "agent-browser/agent-browser"), Buffer.from([0, 255, 0, 254]));
  const records = bundledRuntimeNotices(root);
  expect(records).toHaveLength(3);
  for (const name of ["LICENSE-agent-browser", "LICENSE-axe-core", "LICENSE-axe-core-THIRD-PARTY"]) {
   expect(records).toContain(`\n=== Bundled browser runtime / ${name} ===\nExact supplied ${name}\r\nOriginal copyright and terms\r\n`);
  }
 } finally { rmSync(root, { recursive: true, force: true }); }
});
