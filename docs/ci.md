# Pull request CI

The `CI` workflow runs on every PR to `main`, push to `main` and manual dispatch.
The final job is `required` (shown under `CI` in the checks UI). Desktop packaging
is separate.

## Required checks

| Area | Checks |
| --- | --- |
| Every PR | CI selector/gate tests, actionlint for all workflows, Gitleaks |
| Backend | gofmt, build, vet, golangci-lint 2.13.2, full race tests in existing shards, Windows regression tests, API/sqlc drift |
| Cloud | gofmt, build, vet, race tests, Expat security-backport build |
| Frontend | ESLint, lint regression tests, TypeScript, full Vitest suite, shared client/UI checks, docs build, guarded-navigation integration, renderer smoke, native update-helper tests |
| CLI | Native E2E on Linux/macOS/Windows and fresh-install Docker smoke |
| Mobile | TypeScript and Vitest |
| E2E infrastructure | Pod gate verdict-contract tests; no hosted pod is launched |

Dependencies come from `npm ci`. CI uses Node.js 24 and Go from each module's
`go.mod`. Generator, golangci-lint, actionlint and Playwright container versions
are pinned.

## Selection and failure behavior

`scripts/ci/changes.mjs` compares the event's base commit with the checked-out
merge/HEAD using Git, avoiding API file-count limits. Disabling rename detection
includes both removed and added paths. Missing history, first pushes and manual
runs select all suites. A diff execution error fails the selector. Unknown paths
also select all suites.

Backend changes select backend and CLI; HTTP API producers also select frontend
and mobile consumers. API schema changes select backend, frontend and mobile.
Cloud sources/contracts select Cloud and both clients. Shared client/UI packages
select frontend and mobile. Public backend packages also select Cloud and both
clients; either Go module's requirements select both Go modules and CLI because
they share `go.work` dependency resolution. Root dependency manifests, CI scripts and `.github`
changes select everything. Root Markdown files, `docs/` and translations skip
unaffected application suites; workflow validation, secrets and the final job
still run.

`scripts/ci/required.mjs` checks the selector and all required results. Missing or
malformed outputs, missing results, failures, cancellations and unexpected skips
fail the gate. Only explicitly unaffected suites may be skipped. Additional jobs
included in `needs` must succeed until explicitly integrated with selection.
Cancelling an older run never supplies a successful check for a newer commit.

Reusable test workflows expose `workflow_call` and manual triggers, avoiding
duplicate automatic runs. Other upstream release/catalog/maintenance workflows
retain their triggers and are outside this gate.

## Frontend lint baseline

`frontend/eslint.config.mjs` enables TypeScript recommended correctness rules and
React Hooks order/dependency rules. TypeScript owns unused-variable checks;
explicit `any` remains allowed at existing bridge/test boundaries. Generated API
types and routes, documentation with its own toolchain, dependencies and build
outputs are excluded.

`frontend/eslint-suppressions.json` initially records 144 existing violations in
73 files. ESLint's native baseline stores counts per file/rule. New files and
increases above those counts fail; replacing an old violation with a new one of
the same rule in the same file can stay within the count, so review is still
necessary. CI runs with zero warnings allowed and never expands this baseline.
Do not use `--suppress-all` as a routine fix for a failing PR. After fixing existing
debt, remove stale exceptions with:

```sh
npm --prefix frontend run lint -- --prune-suppressions
```

Regression tests also exercise the real CLI against a new conditional-Hook
violation to check that the baseline does not hide it.

## Local verification

Install the toolchains above, then install dependencies for relevant packages:

```sh
npm ci
npm ci --prefix frontend
npm ci --prefix packages/cloud-client
npm ci --prefix packages/product-ui
npm ci --prefix frontend/src/docs
```

Fast checks:

```sh
npm run ci:test
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.12 -shellcheck=
npm run frontend:lint
npm --prefix frontend run typecheck
npm --prefix frontend run typecheck:e2e
```

Backend, contracts and application tests:

```sh
cd backend
go build ./...
go vet ./...
go test -race -count=1 -timeout=20m ./...
go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.2 run --path-mode=abs
go test -tags e2e -count=1 -timeout=20m -v ./internal/cli/...
cd ..
npm run api
npm run sqlc
git diff --exit-code -- backend/internal/httpd/apispec/openapi.yaml frontend/src/api/schema.ts backend/internal/storage/sqlite/gen
test -z "$(git ls-files --others --exclude-standard -- backend/internal/httpd/apispec/openapi.yaml frontend/src/api/schema.ts backend/internal/storage/sqlite/gen)"
npm run shared:check
npm --prefix frontend run browser-runtime:prepare -- --quiet
npm --prefix frontend test
npm --prefix frontend run test:browser-postcondition-integration
CI=true npm --prefix frontend run test:e2e:renderer
npm run build --prefix frontend/src/docs
node --test scripts/ao-e2e-pod-gate.test.mjs
```

Local Playwright needs Chromium installed from the matching frontend dependency
(`npx playwright install chromium` from `frontend/`). CI supplies browsers in the
pinned container. Unit reports and Playwright HTML/JUnit reports, screenshots and
failure traces have seven-day artifact retention. CI uses two browser workers
and one retry to keep the load bounded. Web-first assertions have a 15-second
budget in CI (5 seconds locally), allowing the lazy renderer modules to finish
loading on hosted runners; successful assertions continue immediately. The
30-second default test deadline and explicit performance thresholds still apply.
Renderer smoke uses a fake
bridge and does not replace packaged-app E2E.

Cloud/mobile, Windows regression, native macOS helper and Docker commands remain
in their respective workflows. Verify them on their actual platforms and report
unavailable environments explicitly. `npx @redwoodjs/agent-ci run --all` is an
additional local workflow runner where Docker and its supported environments are
available. actionlint validates syntax/expressions, not GitHub runner execution.

## Heavy builds and activation

`Desktop build artifacts` remains manual: choose a ref and version to produce
unsigned installers. It needs the `VITE_WORKOS_CLIENT_ID` repository variable,
creates no release and does not publish. Packaged macOS update E2E also remains
manual or caller-triggered. Neither belongs to this PR gate, and no nightly
packaging schedule is introduced.

After the user authorizes pushing this change:

1. Open a PR to `main` and confirm all selected checks pass in GitHub Actions.
2. Verify failure propagation using a deliberately failing lint/test change and
   verify that a docs-only PR reaches a successful final result.
3. Configure a repository ruleset/branch protection requiring the final `required`
   check from the `CI` workflow. Select its exact name from that first run; do not
   require conditional child checks.
4. Verify native runner and Docker jobs unavailable in local validation.

Workflow files do not themselves change GitHub repository rules. Merge protection
is not activated until the first remote runs and this rule are verified.
