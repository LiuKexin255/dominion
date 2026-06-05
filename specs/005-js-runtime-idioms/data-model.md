# Data Model: JavaScript Runtime Packaging and Idiomatic Common APIs

## Runtime Package

Represents a JavaScript package that can be consumed by Node at runtime.

**Fields**:
- `packageName`: canonical package name used by Node module resolution.
- `packageMetadata`: package manifest containing runtime entrypoint and type declaration entrypoint.
- `runtimeFiles`: emitted JavaScript files required at runtime.
- `typeFiles`: emitted declaration files required by TypeScript consumers.
- `directRuntimeDependencies`: packages or npm modules directly imported by runtime files.
- `transitiveRuntimeFiles`: closure of runtime files required by direct dependencies.

**Validation Rules**:
- Must have a runtime entrypoint.
- Must have a type declaration entrypoint when TypeScript declarations are emitted.
- Must not depend on the removed `@dominion/common-js-logs-event` package.

## Runtime Dependency Closure

Represents all modules required to satisfy runtime imports for a service artifact.

**Fields**:
- `directDependencies`: service-declared runtime dependencies.
- `workspacePackages`: shared repository packages in the closure.
- `thirdPartyPackages`: npm packages in the closure.
- `resolvedLayout`: packaged file layout used by Node resolution.

**Validation Rules**:
- Every runtime import in packaged service files resolves within the artifact.
- Indirect dependencies are included through package-owned dependency metadata, not service-level manual enumeration.
- Duplicate package names must resolve predictably without breaking Node resolution semantics.

## Structured Log Record

Represents one emitted log entry.

**Fields**:
- `level`: string severity level.
- `message`: human-readable message text.
- `attributes`: structured queryable fields.
- `timestamp`: emission time for console logs or provider-generated time for OTel logs.

**Validation Rules**:
- Message text must not be replaced by a serialized object body.
- Caller-supplied fields must be available as structured attributes.
- Absent optional error values must not create empty-key fields.

## Service Package Artifact

Represents the deployable JS service package layer.

**Fields**:
- `serviceFiles`: compiled service entrypoint and modules.
- `runtimePackages`: packaged runtime dependency closure.
- `runtimeProtos`: proto source files required by runtime proto loading.
- `entrypoint`: service startup script path.

**Validation Rules**:
- Service entrypoint starts without module-not-found errors.
- Runtime proto files remain available at the expected canonical paths.
- The artifact can be validated locally even when deployment infrastructure is unavailable.
