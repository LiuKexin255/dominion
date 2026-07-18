"""Macro wrapping `js_test` to run vitest via the shared canonical shim.

This is the single supported surface for declaring vitest-based `js_test`
targets in this repository. It encapsulates the per-package delivery of the
canonical hardened shim (`tools/dev/js/run_vitest.mjs`) and the `js_test`
wiring, so callers pass only the code under test and the test-file glob.

Caller `data` contract (FIX B — test against source): the caller's `data`
MUST contain the package's RAW `.ts` source (production + tests), typically
`glob(["src/**/*.ts"])`, NOT the pre-compiled `:lib` target. This is a hard
requirement, not a recommendation.

Why: each package's `tsconfig` pins `"module": "commonjs"`, so the `:lib`
(`ts_project` + `swc`) emits CommonJS (`module.exports`). vitest transforms the
test file through its Vite pipeline (instance A) but does NOT intercept
`require()` inside the already-compiled CJS `:lib` — so the production code
loads shared modules natively (instance B). Production and test then resolve the
*same* source file to *two* module instances, breaking `instanceof` checks and
module-level singletons (e.g. `expect(err).toBeInstanceOf(InvalidTargetError)`
fails even though `name`/`message` match; reporter/default-logger/OTel-tracer
singletons diverge). vitest maintainers confirm this is expected CJS behavior
(vitest#7591: "`require(...)` is not intercepted by Vitest, so the module is
different … current behavior is expected"; vitest#5601: `server.deps.inline`
is the standard lever). Feeding vitest the raw `.ts` source instead makes the
whole package flow through a single Vite pipeline → one module instance ==
the (passing) vitest CLI mode.

CRITICAL invariant: `data` MUST NOT contain BOTH `:lib` and the `.ts` source.
If both the compiled `errors.js` and the source `errors.ts` are present in
runfiles, module resolution is ambiguous (Node/vitest may prefer `.js`),
re-creating the dual instance. Pass ONLY the `.ts` source for the package's
own code. Compile/type correctness is retained — `:lib` (`ts_project`) is still
built as a dependency of `server_pkg`, so `bazel build //...:server_pkg` still
type-checks and compiles the artifact; only what the *test* sees changes.

Why a macro (not a custom rule, not bare genrules): a cross-package
`entry_point` label pointing at `//tools/dev/js:run_vitest.mjs` fails both at
Bazel analysis (the aspect_rules_js `copy_to_bin` constraint requires the
`entry_point` source to live in the consuming package) and at runtime
(`vitest` is only resolvable from a pnpm-workspace package's `node_modules`,
and `tools/dev/js` is not a workspace package). The macro therefore
`genrule`-copies the canonical source into the consuming package — where the
copy lands next to that package's own `node_modules/vitest` — and wires the
`js_test` to use the local generated copy as its `entry_point`. Verified
end-to-end; see:
  specs/019-js-test-reliability/plan.md (Architecture Revision,
    Module-Identity Revision)
  specs/019-js-test-reliability/research.md §3 (delivery), §6 (module identity)

Prerequisite: the calling package MUST already invoke
`npm_link_all_packages(name = "node_modules")` so that the auto-injected
`:node_modules/vitest` resolves at runtime. All six target packages already do.

Shim exit-code contract:
  specs/019-js-test-reliability/contracts/run-vitest-shim.md
"""

load("@aspect_rules_js//js:defs.bzl", "js_test")

def vitest_test(name, data, args = ["run", "src/"], **kwargs):
    """Declares a vitest `js_test` backed by the shared canonical shim.

    Internally emits (a) a `genrule` that copies `//tools/dev/js:run_vitest.mjs`
    into this package as `<name>_run_vitest.mjs`, and (b) a `js_test` whose
    `entry_point` is that generated copy and whose `data` is the caller-supplied
    `data` plus the auto-injected `:node_modules/vitest`. Callers never write the
    `genrule`, the `entry_point`, or the `vitest` data dependency themselves.

    Args:
        name: Name of the test target (e.g. "lib_test").
        data: Runtime data for the test — the package's raw `.ts` source
            (production + tests), typically `glob(["src/**/*.ts"])`, PLUS any
            test-specific `:node_modules/*` deps and non-`.ts` fixtures (e.g.
            proto files). MUST be the source, NOT the pre-compiled `:lib`:
            vitest does not intercept `require()` inside the CJS `:lib`, so
            pairing `:lib` with the test re-creates a dual-module-instance that
            breaks `instanceof`/singletons (see module docstring +
            research.md §6). NEVER include both `:lib` and the `.ts` source.
            The `:node_modules/vitest` dependency is auto-injected; do NOT pass it.
        args: vitest CLI tokens forwarded to the shim. Defaults to
            `["run", "src/"]` (non-watch run filtered to the package's `src/`).
        **kwargs: Forwarded to the underlying `js_test` (e.g. `size`, `tags`).
    """
    native.genrule(
        name = "%s_shim" % name,
        srcs = ["//tools/dev/js:run_vitest.mjs"],
        outs = ["%s_run_vitest.mjs" % name],
        cmd = "cp $< $@",
    )

    js_test(
        name = name,
        entry_point = ":%s_run_vitest.mjs" % name,
        data = data + [":node_modules/vitest"],
        args = args,
        **kwargs
    )
