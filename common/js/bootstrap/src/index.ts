/**
 * @packageDocumentation
 * Barrel exports for the `@dominion/common-js-bootstrap` package.
 *
 * Export surface per `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §1`:
 * value exports (Stage, Bootstrap, factory functions) are named; type
 * re-exports are explicit `export type` (swc transpiles files in isolation,
 * so a value-form re-export of a type would survive into the ESM output and
 * fail linking).
 *
 * @module
 */

// ---------------------------------------------------------------------------
// Component contract
// ---------------------------------------------------------------------------
// Stage is a value+type merged declaration (as const object + literal union),
// so a single value export carries both; Component/ExitWatchable are pure
// types and must be re-exported with `export type`.
export { Stage } from "./component.js";
export type { Component, ExitWatchable } from "./component.js";

// ---------------------------------------------------------------------------
// Orchestrator
// ---------------------------------------------------------------------------
export { Bootstrap } from "./bootstrap.js";
export type { BootstrapOptions, RunOptions } from "./bootstrap.js";

// ---------------------------------------------------------------------------
// Health endpoint types
// ---------------------------------------------------------------------------
export type { HealthHandle, HealthService } from "./health.js";

// ---------------------------------------------------------------------------
// Adapters
// ---------------------------------------------------------------------------
export { createHttpServerComponent } from "./http-server.js";
export type { HttpServerComponentOptions } from "./http-server.js";
export { createGrpcServerComponent } from "./grpc-server.js";
export type { GrpcServerComponentOptions } from "./grpc-server.js";
export { createGrpcConnComponent } from "./grpc-conn.js";

// ---------------------------------------------------------------------------
// Daemon supervisor
// ---------------------------------------------------------------------------
export { createDaemon } from "./daemon.js";
export type { Worker, DaemonDecision, DaemonOptions } from "./daemon.js";
