# Research: JavaScript Runtime Packaging and Idiomatic Common APIs

## Decision: Use direct runtime dependencies plus transitive closure in packaging

**Rationale**: Service BUILD files should declare only what service source directly imports. Requiring services to enumerate shared package indirect dependencies and third-party transitive dependencies is error-prone and contradicts Bazel's dependency model. A runtime package provider or equivalent runfiles-based model lets shared packages own their direct runtime dependencies and lets `artifact_pkg_js` package the closure.

**Alternatives considered**:
- Flat manual `npm_deps`: rejected because OTel and workspace dependencies create a large indirect dependency set and omissions appear only at runtime.
- `lib_deps` that copies only workspace library outputs: rejected because it does not solve workspace-to-workspace or workspace-to-npm transitive dependencies.
- Bundling with esbuild/rollup: deferred because OTel instrumentation relies on module load order and runtime proto loading; bundling is viable only with dedicated validation.

## Decision: Preserve node_modules-style runtime packaging rather than bundle

**Rationale**: The current artifact model is node_modules-style, and the service depends on gRPC instrumentation patching and runtime proto loading. Preserving Node resolution semantics minimizes behavior change while fixing missing runtime dependencies.

**Alternatives considered**:
- Single-file bundle: simpler deployment shape but higher risk for instrumentation and dynamic runtime behavior.
- Running a package-manager install during image build: rejected because repository governance requires Bazel-managed dependency state and reproducible artifacts.

## Decision: Remove standalone `logs/event` package

**Rationale**: Type-specific event constructors and zero-value event sentinels are Go idioms. JavaScript/TypeScript logging ecosystems use plain objects for structured fields. Keeping a separate event package adds a runtime package and packaging dependency without enough value.

**Alternatives considered**:
- Keep `logs/event` for compatibility: rejected because packages are newly introduced and the user explicitly requested removal.
- Keep event constructors in `logs`: accepted only for helpers that add value beyond plain objects, such as optional error-field helpers.

## Decision: Preserve structured OTel attributes separately from message body

**Rationale**: OTel log records support attributes. Encoding all attributes into a JSON body string makes fields harder to query and differs from the Go `otelslog` bridge behavior.

**Alternatives considered**:
- Keep JSON body: rejected because it loses structured queryability.

## Decision: Add package metadata for retained runtime packages

**Rationale**: Node package consumers need package metadata to resolve runtime entrypoints and TypeScript declarations. Repository-local TypeScript path mappings are compile-time only and do not prove deployed Node resolution.

**Alternatives considered**:
- Rely on Bazel path mappings: rejected because the deployed artifact uses Node runtime resolution.

## Decision: Keep CommonJS compatibility for this feature

**Rationale**: The current service and Bazel transpilation path use CommonJS. Switching to ESM/dual output is a broader module-system migration and not necessary to resolve runtime packaging and API idiom issues.

**Alternatives considered**:
- ESM or dual CJS/ESM output: reasonable future work, but outside this cleanup's acceptance scope.
