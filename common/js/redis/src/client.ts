import redis from "redis";

import { ENV_DOMINION_ENVIRONMENT } from "@dominion/common-js-constants";
import { createResolver } from "@dominion/common-js-resolver";
import type { EndpointResolver } from "@dominion/common-js-resolver";

import { deriveStablePassword } from "./credentials.js";
import { parseRedisTarget, resolveTarget } from "./target.js";

const DEFAULT_ENVIRONMENT = "default";
const MAX_PORT = 65535;

/**
 * The node-redis client type. `ReturnType` is used instead of the aliased
 * `RedisClientType` because calling the generic `createClient` pins the RESP
 * generic to its constraint (`RespVersions`) rather than the alias default (2).
 */
export type RedisClient = ReturnType<typeof redis.createClient>;

/**
 * Dependency-injection seams for {@link createRedisClient}
 * (specs/067-redis-infra-support/contracts/common-db-libs.md §2).
 */
export interface RedisClientOptions {
  /** Endpoint resolver override; defaults to `@dominion/common-js-resolver`'s `createResolver()`. */
  resolver?: EndpointResolver;
  /** Environment source; defaults to `process.env` (reads `DOMINION_ENVIRONMENT`). */
  env?: Record<string, string | undefined>;
}

/**
 * Establishes a connected node-redis client for the given dominion target.
 *
 * The target is `app/name`; the default redis port is applied before endpoint
 * resolution. The instance password is derived client-side with the same
 * algorithm the platform injects into the instance. Every failure is surfaced
 * as a plain `Error` (invalid target, no ready endpoint) or the driver's own
 * error (connection failure) — no sentinel error type. Disconnection is the
 * caller's responsibility (`client.disconnect()`).
 */
export async function createRedisClient(
  target: string,
  options: RedisClientOptions = {},
): Promise<RedisClient> {
  const env = options.env ?? process.env;
  const parsed = parseRedisTarget(target);
  const resolver = options.resolver ?? createResolver({ env });

  const endpoints = await resolver.resolve(resolveTarget(parsed));
  if (endpoints.length === 0) {
    throw new Error(
      `no ready endpoints resolved for redis target ${JSON.stringify(target)}`,
    );
  }

  const password = deriveStablePassword([
    parsed.app,
    environmentName(env),
    parsed.service,
  ]);
  const client = redis.createClient({
    socket: parseEndpoint(endpoints[0]!),
    password,
  });
  await client.connect();

  return client;
}

/**
 * Reads the plain environment name used for credential derivation
 * (`DOMINION_ENVIRONMENT`, falling back to `default` for local runs), mirroring
 * the Go library's `environmentName()` in `common/gopkg/redis/client.go`.
 */
function environmentName(env: Record<string, string | undefined>): string {
  return (env[ENV_DOMINION_ENVIRONMENT] ?? "").trim() || DEFAULT_ENVIRONMENT;
}

/** Splits a resolver `host:port` endpoint into node-redis socket options. */
function parseEndpoint(endpoint: string): { host: string; port: number } {
  const separator = endpoint.lastIndexOf(":");
  if (separator <= 0) {
    throw new Error(
      `invalid redis endpoint ${JSON.stringify(endpoint)}: want host:port`,
    );
  }

  const host = endpoint.slice(0, separator).replace(/^\[|\]$/g, "");
  const port = Number(endpoint.slice(separator + 1));
  if (!Number.isInteger(port) || port <= 0 || port > MAX_PORT) {
    throw new Error(
      `invalid redis endpoint ${JSON.stringify(endpoint)}: want host:port`,
    );
  }

  return { host, port };
}
