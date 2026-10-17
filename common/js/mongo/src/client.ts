import { MongoClient } from "mongodb";

import { ENV_DOMINION_ENVIRONMENT } from "@dominion/common-js-constants";
import { createResolver } from "@dominion/common-js-resolver";
import type { EndpointResolver } from "@dominion/common-js-resolver";

import { deriveStablePassword } from "./credentials.js";
import { parseMongoTarget, resolveTarget } from "./target.js";

const DEFAULT_ENVIRONMENT = "default";
const MONGO_USERNAME = "admin";
const MONGO_AUTH_DATABASE = "admin";

/**
 * Dependency-injection seams for the mongo helpers
 * (specs/067-redis-infra-support/contracts/common-db-libs.md §3).
 */
export interface MongoClientOptions {
  /** Endpoint resolver override; defaults to `@dominion/common-js-resolver`'s `createResolver()`. */
  resolver?: EndpointResolver;
  /** Environment source; defaults to `process.env` (reads `DOMINION_ENVIRONMENT`). */
  env?: Record<string, string | undefined>;
}

/**
 * Produces the credentialed `mongodb://` URI for a dominion target without
 * opening a connection (the target syntax and credential rules are documented
 * in specs/067-redis-infra-support/contracts/common-db-libs.md §3).
 *
 * Used by hosts that inject a connection string into a consumer through an
 * environment variable (the agent_v2 preset-authoring pattern, contract §4).
 */
export async function resolveMongoUri(
  target: string,
  options: MongoClientOptions = {},
): Promise<string> {
  const parsed = parseMongoTarget(target);
  const env = options.env ?? process.env;
  const resolver = options.resolver ?? createResolver({ env });

  const endpoints = await resolver.resolve(resolveTarget(parsed));
  if (endpoints.length === 0) {
    throw new Error(
      `no ready endpoints resolved for mongo target ${JSON.stringify(target)}`,
    );
  }

  const password = deriveStablePassword([
    parsed.app,
    environmentName(env),
    parsed.service,
  ]);

  return `mongodb://${MONGO_USERNAME}:${password}@${endpoints[0]!}/${MONGO_AUTH_DATABASE}?authSource=${MONGO_AUTH_DATABASE}`;
}

/**
 * Establishes a connected mongodb client for the given dominion target.
 *
 * Connection is completed with the driver's `connect()` before returning, so
 * a caller receives a ready client; closing is the caller's responsibility
 * (`client.close()`).
 */
export async function createMongoClient(
  target: string,
  options: MongoClientOptions = {},
): Promise<MongoClient> {
  const uri = await resolveMongoUri(target, options);
  const client = new MongoClient(uri);
  await client.connect();
  return client;
}

/**
 * Reads the environment name used for credential derivation
 * (`DOMINION_ENVIRONMENT`, falling back to `default` for local runs), mirroring
 * the Go library's `buildMongoURI` in `common/gopkg/mongo/client.go`.
 */
function environmentName(env: Record<string, string | undefined>): string {
  return (env[ENV_DOMINION_ENVIRONMENT] ?? "").trim() || DEFAULT_ENVIRONMENT;
}
