# vite_react_demo

Minimal vite + React app that proves the repo's standard toolchain (pnpm
workspace + Bazel) can build a React frontend end to end. It is the working
sample of the feature defined in `specs/050-vite-react-bazel/spec.md` and the
prerequisite infrastructure for the web frontend planned in
`specs/049-agent-v2-dsh-init`.

This is a build-infrastructure demo, not an application template: it stays in
its minimal verification shape (no router, state library, UI component
library, or dev-server config — FR-006 boundary, see
`specs/050-vite-react-bazel/spec.md`). It also delivers its dist as a
deployable static-page server: a Go HTTP server embeds the dist tree at build
time and serves it, in the repo's standard service delivery form
(`specs/050-vite-react-bazel/contracts/static-server-deploy.md`).

## Targets and commands

| Target | Rule | What it verifies |
|--------|------|------------------|
| `:dist` | `vite_build` | Builds the app into a dist tree artifact (`index.html` + content-hash-named assets under `assets/`) |
| `:lib_test` | `vitest_test` | Component unit tests (React Testing Library + jsdom): marker string renders, counter interaction increments |
| `:dist_assert_test` | `sh_test` | Dist artifact assertions A1–A4 per `specs/050-vite-react-bazel/contracts/dist-artifact-assertions.md` |
| `server/:server` | `go_binary` | Static-page server embedding the `:dist` tree (`wails_asset_library` embed lib in `server/assets/`) |
| `server/:cmd_image` | `artifact_image` | OCI image of the server (via `server/:server_pkg`), the deploy artifact referenced by `server/service.yaml` |
| `server/:server_test` | `go_unittest` | Server unit tests: `/` serves the entry HTML with `/assets/` references, hashed assets are served, unknown paths 404 |

```bash
bazel build //experimental/js/vite_react_demo:dist
bazel test //experimental/js/vite_react_demo:lib_test --test_output=all
bazel test //experimental/js/vite_react_demo:dist_assert_test --test_output=all
bazel build //experimental/js/vite_react_demo/server:cmd_image
bazel test //experimental/js/vite_react_demo/server:server_test --test_output=all
```

The full acceptance script (build + tests + no-regression + repeatable clean
rebuild + dependency-governance check + deploy/verify/cleanup loop) is
`specs/050-vite-react-bazel/quickstart.md`, scenarios 1–7.

## Prerequisites

The package is a pnpm workspace member (`pnpm-workspace.yaml` packages cover
`experimental/js/*`) and declares all dependencies as `catalog:` references.

After a fresh clone or any lockfile change, install `node_modules` first:

```bash
bazel run @pnpm -- --dir /mnt/code/dominion
```

Why builds fail without it: frontend build rules execute **locally** against
the source-tree `node_modules` — `vite_build` cd's into the package directory
and invokes `./node_modules/.bin/vite build` directly
(`tools/dev/js/vite.bzl:48`, `execution_requirements = {"local": ""}` at
`tools/dev/js/vite.bzl:56`). When `node_modules` is absent the binary does not
exist and the build fails with `No such file or directory`. This matches the
existing behavior of all frontend consumers by design (no extra environment
assumptions — Edge Case「本地执行环境差异」in `specs/050-vite-react-bazel/spec.md`);
running the pnpm command above resolves it.

## Deploying the demo

The demo deploys through the repo's deploy tool (`tools/release/deploy`).
Install the CLI first (installs to `$HOME/.local/bin` by default):

```bash
bazel run //:deploy_install
```

Deploy, verify, and clean up (`deploy.yaml` at the demo root declares the
environment):

```bash
# Build + push the image (registry.liukexin.com), apply environment
# vite.demo, and wait until it is ready. Keeps running after apply.
deploy apply //experimental/js/vite_react_demo/deploy.yaml

# Entry URL — open in a browser to verify the page renders
# (marker text + counter interaction, assets all load).
https://vite-react-demo.liukexin.com/

# Remove the environment and its routes when done.
deploy del vite.demo
```

Notes:

- `deploy.yaml` declares environment `vite.demo` with `type: prod` and
  hostname `vite-react-demo.liukexin.com`. `type: prod` here only selects the
  header-free direct-routing access mode (hostname + path); it is not a
  business production environment — access semantics and rationale in
  `specs/050-vite-react-bazel/contracts/static-server-deploy.md` §2.3.
- `deploy apply` requires `registry.liukexin.com` push credentials, a
  platform prerequisite shared by all repo demo services
  (`specs/050-vite-react-bazel/spec.md` Edge Cases「镜像推送凭证」).
- curl-equivalent verification: the entry URL returns the HTML with
  `/assets/` references; any referenced `assets/*.js` returns 200 and its
  content contains `dominion-vite-react-demo` (same source as assertion A4).

## Large-test exemption (Constitution VI)

Per `.specify/memory/constitution.md` Principle VI (exemption clause), the
large-test status of this package is documented here:

- **The delivered service is unit-tested and its deployment capability is
  accepted by real execution.** The demo ships a static-page server
  (service-type delivery) with unit coverage in `server:server_test` (entry
  HTML, asset serving, 404 behavior). Deployment is verified by actually
  executing quickstart scenario 7 — `deploy apply` → entry-URL verification →
  `deploy del` cleanup — per
  `specs/050-vite-react-bazel/quickstart.md`.
- **Web E2E large tests are deferred by user decision** (2026-08-27, per
  `specs/050-vite-react-bazel/spec.md` FR-009). When introduced later, they
  join through the testplan/guitar loop reusing the same `service.yaml` and a
  test-type deploy config, with no change to the service form
  (`specs/050-vite-react-bazel/contracts/static-server-deploy.md`
  §"与 guitar / testplan 的关系").

## Consuming this setup for new React frontends (049 and beyond)

Any vite + React frontend joins the repo's Bazel build through the same
contract this demo implements — target declaration, attribute values,
prerequisites, and the component-test `data` mirroring rules are specified in
`specs/050-vite-react-bazel/contracts/vite-build-target.md`. New projects need
no changes to the build rules themselves.

A web service that serves its frontend directly (the shape planned for 049's
web service) reuses the same dist tree as a deployable carrier: Go embed
server + `service.yaml`/`deploy.yaml` + deploy CLI, as specified in
`specs/050-vite-react-bazel/contracts/static-server-deploy.md`.
