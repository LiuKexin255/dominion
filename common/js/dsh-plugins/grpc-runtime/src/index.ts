/**
 * cordis plugin entry for the composition-level gRPC client runtime.
 *
 * One row per composition, domain-agnostic: registers the dominion URI
 * resolver (idempotent) and provides `ctx.grpc`
 * (specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md §1/§2/§4).
 *
 * Ordering: the OTel ESM loader hook and the gRPC instrumentation are
 * registered by the service bootstrap's `init()` before the composition (and
 * therefore this plugin) is dynamically imported
 * (specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md
 * §2); this module MUST NOT create a channel at load time, so the hook is
 * already active when `@grpc/grpc-js` enters the module graph. Channel
 * creation is lazy — only `ctx.grpc.client(...)` builds one — and the resolver
 * is registered first in {@link apply}.
 */

import type { Context } from "@deepseek-ai/cordis";

import { registerDominionResolver } from "@dominion/common-js-grpc-resolver";

import { createGrpcRuntime } from "./client.js";
import type { GrpcRuntime, GrpcRuntimeHandle } from "./client.js";

/** The plugin identifier the composition manifest references. */
export const name = "grpc-runtime";

/** Pure provider: no service requirements. */
export const inject: string[] = [];

/**
 * Injection seams for {@link apply} (DI; tests pass `vi.fn()` doubles and
 * assert they are exercised — style/javascript.md §测试).
 */
export interface GrpcRuntimePluginDeps {
  /** Idempotent resolver registration (defaults to the real resolver). */
  readonly registerResolver?: () => void;
  /** Runtime factory; owns the channel cache (defaults to the real runtime). */
  readonly createRuntime?: () => GrpcRuntimeHandle;
}

/**
 * Apply the plugin: register the dominion resolver and the gRPC
 * instrumentation ordering prerequisite, provide `ctx.grpc`, and register the
 * channel teardown on the composition lifecycle.
 */
export function apply(ctx: Context, deps: GrpcRuntimePluginDeps = {}): void {
  // The second parameter is a DI seam for tests. This composition row MUST
  // carry no config (specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md §1),
  // so a slot holding a non-function value (e.g. an accidental config object)
  // falls back to the production default instead of failing obscurely.
  const registerResolver =
    typeof deps.registerResolver === "function"
      ? deps.registerResolver
      : registerDominionResolver;
  const createRuntime =
    typeof deps.createRuntime === "function"
      ? deps.createRuntime
      : createGrpcRuntime;

  registerResolver();

  const handle = createRuntime();
  ctx.provide("grpc", handle.runtime);
  ctx.effect(() => () => handle.close(), "grpc-runtime.channels");
}

declare module "@deepseek-ai/cordis" {
  interface Context {
    grpc: GrpcRuntime;
  }
}

export {
  createGrpcRuntime,
  resolveProtoPath,
} from "./client.js";
export type {
  GrpcClientOptions,
  GrpcRuntime,
  GrpcRuntimeDeps,
  GrpcRuntimeHandle,
} from "./client.js";
export { callUnary, probeChannel } from "./unary.js";
export type { UnaryInvoke } from "./unary.js";
export {
  ChannelPool,
  RECONNECT_OPTIONS,
  ROUND_ROBIN_SERVICE_CONFIG,
  TLS_CA_CERT,
  buildChannelOptions,
  buildClientCredentials,
} from "./channel.js";
