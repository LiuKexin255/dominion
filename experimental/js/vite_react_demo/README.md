# vite_react_demo

Minimal vite + React app that proves the repo's standard toolchain (pnpm
workspace + Bazel) can build a React frontend end to end. It is the working
sample of the feature defined in `specs/050-vite-react-bazel/spec.md` and the
prerequisite infrastructure for the web frontend planned in
`specs/049-agent-v2-dsh-init`.

This is a build-infrastructure demo, not an application template: it stays in
its minimal verification shape (no router, state library, UI component
library, or dev-server config — FR-006 boundary, see
`specs/050-vite-react-bazel/spec.md`).

## Targets and commands

| Target | Rule | What it verifies |
|--------|------|------------------|
| `:dist` | `vite_build` | Builds the app into a dist tree artifact (`index.html` + content-hash-named assets under `assets/`) |
| `:lib_test` | `vitest_test` | Component unit tests (React Testing Library + jsdom): marker string renders, counter interaction increments |
| `:dist_assert_test` | `sh_test` | Dist artifact assertions A1–A4 per `specs/050-vite-react-bazel/contracts/dist-artifact-assertions.md` |

```bash
bazel build //experimental/js/vite_react_demo:dist
bazel test //experimental/js/vite_react_demo:lib_test --test_output=all
bazel test //experimental/js/vite_react_demo:dist_assert_test --test_output=all
```

The full acceptance script (build + both tests + no-regression + repeatable
clean rebuild + dependency-governance check) is
`specs/050-vite-react-bazel/quickstart.md`, scenarios 1–6.

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

## Large-test exemption (Constitution VI)

Per `.specify/memory/constitution.md` Principle VI, this package claims the
README-documented exemption from large tests:

- **It is not a delivered service.** There is no runnable service process to
  deploy, so the deploy→test→cleanup loop of a testplan-based large test has
  nothing to act on.
- **Its acceptance is `bazel build` + `bazel test`**: the dist assertions
  (A1–A4) prove the built artifact is complete and really contains compiled
  React code, and the component unit tests prove the app logic works — the
  full evidence chain runs automatically with scenarios 1–6 of
  `specs/050-vite-react-bazel/quickstart.md`.

## Consuming this setup for new React frontends (049 and beyond)

Any vite + React frontend joins the repo's Bazel build through the same
contract this demo implements — target declaration, attribute values,
prerequisites, and the component-test `data` mirroring rules are specified in
`specs/050-vite-react-bazel/contracts/vite-build-target.md`. New projects need
no changes to the build rules themselves.
