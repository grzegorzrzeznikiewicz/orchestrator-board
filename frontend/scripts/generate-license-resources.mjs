// Modified for Orchestrator Board by Gama Software, 2026 (OB-1).

import { createHash } from "node:crypto";
import { execFileSync } from "node:child_process";
import { existsSync, lstatSync, mkdirSync, readdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import ts from "typescript";

const frontendRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repositoryRoot = path.dirname(frontendRoot);
const legalName = /^(?:(?:third[. _-]*party|3rd[. _-]*party)[. _-]*)?(?:licen[sc]es?|copying|notices?|copyright(?:notice)?|authors?|contributors?)(?:text)?([. _-]|$)/i;

export function legalTexts(directory, includeAll = false) {
 if (!existsSync(directory)) return [];
 const texts = [];
 for (const name of readdirSync(directory).sort()) {
  if (["node_modules", ".git"].includes(name)) continue;
  const file = path.join(directory, name);
  const info = lstatSync(file);
  if (info.isSymbolicLink()) continue;
  // Executable files such as icons/copyright.mjs are source, not standalone
  // notices. Their actual legal comments are retained by embeddedLegalTexts.
  if (info.isFile() && !sourceExtension.test(name) && !/\.map$/i.test(name) && (includeAll || legalName.test(name))) texts.push({ name, text: readFileSync(file, "utf8") });
  if (info.isDirectory()) {
   for (const entry of legalTexts(file, includeAll || /^(licenses?|copyrights?)$/i.test(name))) texts.push({ name: `${name}/${entry.name}`, text: entry.text });
  }
 }
 return texts;
}


const sourceExtension = /\.(?:[cm]?[jt]sx?|go|[ch](?:pp|xx)?|cc|css|scss|s|rs|m|mm)$/i;
const legalComment = /\bcopyright\b(?!\s*[,;])|©|\blicen[cs](?:e|ed|ing)\b|\bpermission\b|\bSPDX-License-Identifier\b/i;

// Go/C-style source does not need JavaScript's regular-expression parsing.
function cStyleSourceComments(source) {
 const comments = [];
 for (let offset = 0; offset < source.length;) {
  const start = offset;
  const character = source[offset];
  if (character === '"' || character === "'" || character === "`") {
   offset++;
   while (offset < source.length) {
    if (source[offset] === "\\") { offset += 2; continue; }
    if (source[offset++] === character) break;
   }
   continue;
  }
  if (character === "\\") { offset += 2; continue; }
  if (source.startsWith("/*", offset)) {
   const end = source.indexOf("*/", offset + 2);
   if (end < 0) break;
   offset = end + 2;
  } else if (source.startsWith("//", offset)) {
   // Keep whole contiguous comment paragraphs, including every term of a
   // line-commented license. Blank comment lines remain part of the notice.
   do {
    const end = source.indexOf("\n", offset);
    offset = end < 0 ? source.length : end;
    const next = /^\r?\n[\t ]*\/\//.exec(source.slice(offset));
    if (!next) break;
    offset += next[0].length;
   } while (offset < source.length);
  } else { offset++; continue; }
  const text = source.slice(start, offset);
  if (legalComment.test(text)) comments.push({ line: source.slice(0, start).split("\n").length, text });
 }
 return comments;
}

function sourceComments(source, filename) {
 if (!legalComment.test(source)) return [];
 if (!/\.[cm]?[jt]sx?$/i.test(filename)) return cStyleSourceComments(source);
 // Use the installed build-time parser: quotes in regexes and comment-looking
 // strings/templates must never hide or invent an original source notice.
 const tree = ts.createSourceFile(filename, source, ts.ScriptTarget.Latest, false);
 const ranges = new Map();
 const pending = [tree];
 while (pending.length) {
  const node = pending.pop();
  for (const range of [...(ts.getLeadingCommentRanges(source, node.pos) ?? []), ...(ts.getTrailingCommentRanges(source, node.end) ?? [])]) ranges.set(range.pos, range);
  ts.forEachChild(node, (child) => { pending.push(child); });
 }
 const paragraphs = [];
 for (const range of [...ranges.values()].sort((a, b) => a.pos - b.pos)) {
  const previous = paragraphs.at(-1);
  if (previous?.kind === ts.SyntaxKind.SingleLineCommentTrivia && range.kind === previous.kind && /^\r?\n[\t ]*$/.test(source.slice(previous.end, range.pos))) previous.end = range.end;
  else paragraphs.push({ ...range });
 }
 return paragraphs.map(({ pos, end }) => ({ pos, text: source.slice(pos, end) })).filter(({ text }) => legalComment.test(text)).map(({ pos, text }) => ({ line: source.slice(0, pos).split("\n").length, text }));
}

export function embeddedLegalTexts(directory, selectedFiles) {
 const files = [];
 const visit = (relative = "") => {
  for (const name of readdirSync(path.join(directory, relative)).sort()) {
   if (["node_modules", ".git"].includes(name)) continue;
   const file = path.join(relative, name);
   const info = lstatSync(path.join(directory, file));
   if (info.isSymbolicLink()) continue;
   if (info.isDirectory()) visit(file);
   else if (info.isFile() && (sourceExtension.test(name) || /\.map$/i.test(name))) files.push(file);
  }
 };
 if (selectedFiles) {
  for (const file of [...new Set(selectedFiles)].sort()) {
   const relative = path.relative(directory, file);
   if (relative.startsWith(`..${path.sep}`) || path.isAbsolute(relative)) throw new Error("Embedded notice source escapes module directory");
   if (sourceExtension.test(relative) && lstatSync(file).isFile()) files.push(relative);
  }
 } else visit();
 const notices = new Map();
 for (const relative of files) {
  const source = readFileSync(path.join(directory, relative), "utf8");
  if (source.includes("\0")) continue;
  const contents = [];
  if (/\.map$/i.test(relative)) {
   let map;
   try { map = JSON.parse(source); } catch { continue; }
   if (Array.isArray(map?.sourcesContent)) {
    map.sourcesContent.forEach((text, index) => {
     if (typeof text === "string") contents.push({ source: text, filename: typeof map.sources?.[index] === "string" ? map.sources[index] : `source-${index}.ts`, origin: `${relative} -> ${typeof map.sources?.[index] === "string" ? map.sources[index] : `sourcesContent[${index}]`}` });
    });
   }
  } else contents.push({ source, filename: relative, origin: relative });
  for (const content of contents) {
   for (const { line, text } of sourceComments(content.source, content.filename)) {
    const location = `${content.origin.split(path.sep).join("/")}:${line}`;
    const existing = notices.get(text);
    if (existing) existing.locations.push(location);
    else notices.set(text, { text, locations: [location] });
   }
  }
 }
 return [...notices.values()].map(({ text, locations }) => ({ name: `embedded source notice: ${locations.join(", ")}`, text }));
}

function readmeLegalTexts(directory) {
 const notices = [];
 for (const name of readdirSync(directory).filter((entry) => /^readme(?:[._-]|$)/i.test(entry)).sort()) {
  if (!lstatSync(path.join(directory, name)).isFile()) continue;
  const text = readFileSync(path.join(directory, name), "utf8");
  const lines = text.split(/(?<=\n)/);
  const headings = [];
  const standalone = [];
  let offset = 0;
  let fence;
  for (let index = 0; index < lines.length; index++) {
   const line = lines[index].replace(/\r?\n$/, "");
   const marker = /^ {0,3}(`{3,}|~{3,})/.exec(line);
   if (marker) {
    if (!fence) fence = marker[1];
    else if (marker[1][0] === fence[0] && marker[1].length >= fence.length && !line.slice(marker[0].length).trim()) fence = undefined;
   } else if (!fence) {
    const atx = /^ {0,3}(#{1,6})[\t ]+(.+?)[\t ]*#*[\t ]*$/.exec(line);
    const underline = /^ {0,3}(={3,}|-{3,})[\t ]*\r?\n?$/.exec(lines[index + 1] ?? "");
    if (atx) headings.push({ offset, depth: atx[1].length, title: atx[2] });
    else if (line.trim() && underline) headings.push({ offset, depth: underline[1][0] === "=" ? 1 : 2, title: line.trim() });
    if (/^ {0,3}(?:copyright\b|©[\t ]*\d{4})/i.test(line)) standalone.push({ name: `${name} standalone copyright notice`, text: lines[index] });
   }
   offset += lines[index].length;
  }
  for (let index = 0; index < headings.length; index++) {
   const heading = headings[index];
   if (!/^(?:(?:the|mit|bsd|apache)\s+)?(?:licen[cs](?:e|ing)|copyright|legal|authors?|contributors?)\b/i.test(heading.title)) continue;
   const next = headings.slice(index + 1).find((entry) => entry.depth <= heading.depth);
   notices.push({ name: `${name} legal section: ${heading.title}`, text: text.slice(heading.offset, next?.offset ?? text.length) });
  }
  for (const entry of standalone) {
   if (!notices.some((notice) => notice.text.includes(entry.text))) notices.push(entry);
  }
 }
 return notices;
}

export function collectNpmNotices(root, overrides = {}, inheritedLicense = "") {
 const lock = JSON.parse(readFileSync(path.join(root, "package-lock.json"), "utf8"));
 const entries = [];
 for (const [relative, info] of Object.entries(lock.packages).sort(([a], [b]) => a.localeCompare(b))) {
  if (!relative || info.dev || info.link) continue;
  const directory = path.resolve(root, relative);
  const manifestPath = path.join(directory, "package.json");
  if (!existsSync(manifestPath)) {
   if (info.optional) continue; // Absent optional platform packages are not distributed.
   throw new Error(`Required production package is not installed: ${relative}`);
  }
  const manifest = JSON.parse(readFileSync(manifestPath, "utf8"));
  let texts = legalTexts(directory);
  if (!texts.length) {
   const readme = readdirSync(directory).find((name) => /^readme/i.test(name));
   if (readme) {
    const text = readFileSync(path.join(directory, readme), "utf8");
    if (/permission is hereby granted/i.test(text)) texts = [{ name: readme, text }];
   }
  }
  if (!texts.length && overrides[manifest.name]) {
   texts = [...overrides[manifest.name]];
   if (texts.some((entry) => entry.packageVersion !== manifest.version)) throw new Error(`License snapshot version mismatch: ${manifest.name}@${manifest.version}`);
  }
  if (!texts.length && !relative.includes("node_modules/") && inheritedLicense) texts = [{ name: "repository LICENSE (local source; package metadata retained below)", text: inheritedLicense }];
  if (!texts.length) throw new Error(`Missing full license text for ${manifest.name}@${manifest.version}`);
  texts.push(...readmeLegalTexts(directory), ...embeddedLegalTexts(directory));
  const author = typeof manifest.author === "object" ? JSON.stringify(manifest.author) : manifest.author;
  entries.push(`\n=== ${manifest.name}@${manifest.version} ===\nDeclared SPDX/package license: ${JSON.stringify(manifest.license ?? info.license ?? "not supplied")}\nAuthor: ${author ?? "see notices below"}\nSource: ${JSON.stringify(manifest.repository ?? manifest.homepage ?? "not supplied")}\n${texts.map(({ name, text }) => `\n--- ${name} ---\n${text}`).join("\n")}`);
 }
 return entries;
}

function readOverrides() {
 const directory = path.join(frontendRoot, "scripts/license-overrides");
 const manifest = JSON.parse(readFileSync(path.join(directory, "sources.json"), "utf8"));
 const entries = {};
 for (const [name, source] of Object.entries(manifest)) {
  const text = readFileSync(path.join(directory, source.file), "utf8");
  if (createHash("sha256").update(text).digest("hex") !== source.sha256) throw new Error(`License snapshot checksum mismatch: ${name}`);
  entries[name] = [{ name: `retained license snapshot for ${name}@${source.packageVersion}; ${source.source}; SHA-256 ${source.sha256}${source.evidenceGap ? `; EVIDENCE GAP: ${source.evidenceGap}` : ""}`, text, packageVersion: source.packageVersion }];
 }
 return entries;
}


export function bundledRuntimeNotices(root) {
 return [
  ...legalTexts(path.join(root, "tmux")).map(({ name, text }) => `\n=== Bundled terminal runtime / ${name} ===\n${text}`),
  ...legalTexts(path.join(root, "agent-browser")).map(({ name, text }) => `\n=== Bundled browser runtime / ${name} ===\n${text}`),
 ];
}


export function generateLicenseResources() {
 const output = path.join(frontendRoot, "resources/licenses");
 mkdirSync(output, { recursive: true });
 const license = readFileSync(path.join(repositoryRoot, "LICENSE"), "utf8");
 const notices = collectNpmNotices(frontendRoot, readOverrides(), license);
 const acp = path.join(frontendRoot, "resources/acp-runtime");
 if (existsSync(path.join(acp, "package-lock.json"))) {
  notices.push(...collectNpmNotices(acp, readOverrides(), license));
  for (const file of legalTexts(path.join(acp, "node"))) notices.push(`\n=== Packaged Node.js runtime / ${file.name} ===\n${file.text}`);
 }
 const go = process.env.OB_LICENSE_GO || "go";
 const moduleOutput = execFileSync(go, ["list", "-deps", "-f", "{{if .Module}}{{.Module.Path}}|{{.Module.Version}}|{{.Module.Dir}}|{{.Module.Main}}|{{.Dir}}|{{range .GoFiles}}{{.}},{{end}}{{range .CgoFiles}}{{.}},{{end}}{{range .CFiles}}{{.}},{{end}}{{range .HFiles}}{{.}},{{end}}{{range .SFiles}}{{.}},{{end}}{{end}}", "./cmd/ao"], { cwd: path.join(repositoryRoot, "backend"), env: { ...process.env, GOPROXY: "off", GOSUMDB: "off" }, encoding: "utf8" });
 const modules = new Map();
 for (const line of moduleOutput.trim().split("\n").filter(Boolean)) {
  const [modulePath, version, directory, main, packageDirectory, names] = line.split("|");
  if (main === "true" || !directory) continue;
  const module = modules.get(modulePath) ?? { version, directory, files: [] };
  module.files.push(...names.split(",").filter(Boolean).map((name) => path.join(packageDirectory, name)));
  modules.set(modulePath, module);
 }
 for (const [modulePath, module] of [...modules].sort(([a], [b]) => a.localeCompare(b))) {
  const texts = legalTexts(module.directory);
  if (!texts.length) throw new Error(`Missing Go module license: ${modulePath}@${module.version}`);
  texts.push(...embeddedLegalTexts(module.directory, module.files));
  notices.push(`\n=== Go ${modulePath}@${module.version} ===\n${texts.map(({ name, text }) => `--- ${name} ---\n${text}`).join("\n")}`);
 }
 const goRoot = execFileSync(go, ["env", "GOROOT"], { encoding: "utf8" }).trim();
 notices.push(`\n=== Go standard library ===\n${readFileSync(path.join(goRoot, "LICENSE"), "utf8")}`);
 notices.push(...bundledRuntimeNotices(frontendRoot));
 const assetNotices = ["LICENSE-mimo-code.txt", "LICENSE-codewhale.txt"].map((name) => `\n=== ${name} ===\n${readFileSync(path.join(frontendRoot, "src/renderer/assets/agents", name), "utf8")}`).join("\n");
 writeFileSync(path.join(output, "LICENSE"), license);
 writeFileSync(path.join(output, "Electron-LICENSE.txt"), readFileSync(path.join(frontendRoot, "node_modules/electron/dist/LICENSE")));
 writeFileSync(path.join(output, "Chromium-LICENSES.html"), readFileSync(path.join(frontendRoot, "node_modules/electron/dist/LICENSES.chromium.html")));
 writeFileSync(path.join(output, "THIRD-PARTY-LICENSES.txt"), "Orchestrator Board — distributed dependency licenses\nGenerated offline from installed production lockfile entries, bundled ACP runtime, and the Go module graph.\nElectron and Chromium additionally ship their original LICENSE and LICENSES.chromium.html beside the application resources.\n\n" + notices.join("\n"));
 writeFileSync(path.join(output, "ASSET-NOTICES.txt"), "Gama Software artwork supplied and authorized by the project owner on 2026-10-17.\nEditable adaptation: frontend/assets/orchestrator-board.svg (three Gama diamonds plus workflow board); derived PNG/ICNS/ICO/tray and renderer assets.\nSources: company-site assets/images/gama-software-logo.png; db-anonymizer packaging/icons/db-anonymizer.svg.\nGama Software name and artwork remain their owner's marks. See docs/fork-branding-assets.md for source hashes and scope.\nExisting agent marks retain their respective owners and licenses.\n" + assetNotices);
 writeFileSync(path.join(output, "NOTICE"), readFileSync(path.join(frontendRoot, "scripts/fork-NOTICE.txt"), "utf8"));
 console.log(`Wrote ${output} (${notices.length} dependency/runtime records)`);
}

if (process.argv[1] && path.resolve(process.argv[1]) === fileURLToPath(import.meta.url)) generateLicenseResources();
