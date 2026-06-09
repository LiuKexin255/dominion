import type { Target, PortSelector } from "./target";
import type { Endpoint } from "@grpc/grpc-js/build/src/subchannel-address";

export type { Target, PortSelector };

/**
 * Minimal fetch-compatible signature for injection.
 */
export type FetchLike = (input: string, init?: RequestInit) => Promise<Response>;

/**
 * Timer scheduler interface, patterned on the global scheduler contract
 * so that real usage passes through to `setInterval` / `clearInterval`
 * while tests can inject a virtual clock.
 */
export interface Scheduler {
  setInterval(callback: () => void, ms: number): unknown;
  clearInterval(handle: unknown): void;
}

/**
 * Plain configuration object for resolver factories and grpc-js
 * registration helpers.
 *
 * All fields are optional – sensible defaults are applied at
 * factory / registration time.
 */
export interface ResolverConfig {
  deployBaseUrl?: string;
  fetch?: FetchLike;
  env?: Record<string, string | undefined>;
  refreshIntervalMs?: number;
  scheduler?: Scheduler;
}

/**
 * Deploy API response model after proto‑JSON decoding.
 *
 * Maps directly to the camelCase transform of the proto fields
 * `endpoints`, `ports`, `is_stateful`, `stateful_instances`.
 */
export interface ServiceEndpoints {
  endpoints: string[];
  ports: Record<string, number>;
  isStateful: boolean;
  statefulInstances: StatefulInstance[];
}

/**
 * A single ordinal instance of a stateful service.
 */
export interface StatefulInstance {
  index: number;
  endpoints: string[];
  hostname?: string;
}

/**
 * Runtime environment derived from the `DOMINION_ENVIRONMENT` variable
 * in `scope.envName` format.
 */
export interface DominionEnvironment {
  scope: string;
  environment: string;
}

/**
 * Internal resolver state machine.
 *
 * - `unresolved` → `ready` after first successful deploy resolution.
 * - `ready` stays `ready` on refresh (identical or updated addresses)
 *    or on failure (last addresses retained, error reported).
 * - Any state → `closed` when the resolver is destroyed.
 */
export type ResolverState =
  | { status: "unresolved" }
  | {
      status: "ready";
      addresses: string[];
      endpoints: Endpoint[];
      lastUpdatedAt: Date;
    }
  | { status: "closed" };

/**
 * Direct endpoint resolver.
 */
export interface EndpointResolver {
  resolve(target: string | Target): Promise<string[]>;
}

/**
 * Stateful endpoint resolver (ordinal instance selection).
 */
export interface StatefulEndpointResolver {
  resolveInstance(target: string | Target, instance: number): Promise<string[]>;
}
