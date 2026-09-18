/**
 * presets.ts — the agent_v2 side of the preset persistence wiring: the
 * deployment Mongo connection and credential derivation the preset-authoring
 * plugin's Mongo store consumes (T006, specs/059-agent-v2-team-mode/
 * tasks.md: "收缩为 Mongo 连接/凭据供 authoring Store 复用"). The generic
 * plugin carries no Dominion deployment logic; the bootstrap resolves the
 * credentialed URI here (same-source with the Go services) and injects it
 * into the composition row via the `MONGO_URI` environment variable — the
 * established host-side injection pattern (GLM_BASE_URL/GLM_API_KEY,
 * projects/game/agent_v2/src/dsh.ts).
 */

import { createHmac } from "node:crypto";

import { ENV_DOMINION_ENVIRONMENT } from "@dominion/common-js-constants";
import { createResolver } from "@dominion/common-js-resolver";
import type { EndpointResolver } from "@dominion/common-js-resolver";

/** The agent_v2 service's own Mongo database (style/mongo.md isolation). */
export const PRESET_DATABASE = "game_agent_v2";

/** The preset collection inside {@link PRESET_DATABASE}. */
export const PRESET_COLLECTION_NAME = "presets";

/** Logical Dominion target backing agent_v2's preset storage. */
export const MONGO_TARGET = "dominion:///game/mongo:27017";

// ── Deployment Mongo credential derivation ──────────────────────────────────
//
// The platform MongoDB instances authenticate (admin user), and every Go
// service of the app connects with a deterministic admin password derived
// from the deploy environment (dominion/common/gopkg/mongo/credentials.go
// generateStablePassword + client.go buildMongoURI). The derivation below is
// byte-for-byte the Go algorithm so agent_v2 authenticates exactly like the
// Go services against the same instance; a direct MONGO_URI keeps precedence.

const MONGO_PASSWORD_HMAC_KEY = "dominion-mongo-stable-password";
const MONGO_PASSWORD_ALPHABET =
  "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
const MONGO_PASSWORD_MIN_LEN = 24;
const MONGO_PASSWORD_JOINER = "\x00";
const DEFAULT_MONGO_ENVIRONMENT = "default";
const MONGO_USERNAME = "admin";
const MONGO_AUTH_DATABASE = "admin";

/**
 * Derive the deployment Mongo admin password — byte-for-byte the Go
 * algorithm (credentials.go): HMAC-SHA256 over the trim-normalized inputs
 * joined with NUL, each digest byte mapped onto the alphanumeric alphabet;
 * a digest shorter than the minimum length is extended by re-walking the
 * digest bytes.
 */
function deriveStableMongoPassword(inputs: string[]): string {
  const normalized = inputs.map((input) => input.trim());
  const mac = createHmac("sha256", MONGO_PASSWORD_HMAC_KEY);
  mac.update(normalized.join(MONGO_PASSWORD_JOINER));
  const sum = mac.digest();
  const encoded: string[] = [];
  for (const byte of sum) {
    encoded.push(MONGO_PASSWORD_ALPHABET[byte % MONGO_PASSWORD_ALPHABET.length] ?? "");
  }
  for (const byte of sum) {
    while (encoded.length < MONGO_PASSWORD_MIN_LEN) {
      encoded.push(MONGO_PASSWORD_ALPHABET[byte % MONGO_PASSWORD_ALPHABET.length] ?? "");
    }
  }
  return encoded.join("");
}

/** The `app`/`service` halves of a {@link MONGO_TARGET}-shaped target. */
function mongoTargetParts(target: string): { app: string; service: string } {
  const bare = target.replace(/^dominion:\/\/\//, "");
  const withoutPort = bare.slice(0, bare.lastIndexOf(":"));
  const [app = "", service = ""] = withoutPort.split("/");
  return { app, service };
}

/**
 * Endpoint precedence: `MONGO_URI` as-is (tests/local direct connect) > the
 * Dominion resolver answer for {@link MONGO_TARGET}, turned into a
 * credentialed `mongodb://` URI (the derived credential is same-source with
 * the Go services'). Injectable for tests (style/javascript.md Mock
 * convention).
 */
export async function resolveMongoUri(
  deps: { env?: Record<string, string | undefined>; resolver?: EndpointResolver } = {},
): Promise<string> {
  const env = deps.env ?? process.env;
  const direct = env.MONGO_URI;
  if (direct) {
    return direct;
  }
  const resolver = deps.resolver ?? createResolver();
  const endpoints = await resolver.resolve(MONGO_TARGET);
  if (endpoints.length === 0) {
    throw new Error(`resolver returned no endpoints for ${MONGO_TARGET}`);
  }
  const envName = (env[ENV_DOMINION_ENVIRONMENT] ?? "").trim() || DEFAULT_MONGO_ENVIRONMENT;
  const { app, service } = mongoTargetParts(MONGO_TARGET);
  const password = deriveStableMongoPassword([app, envName, service]);
  return `mongodb://${MONGO_USERNAME}:${password}@${endpoints[0]}/${MONGO_AUTH_DATABASE}?authSource=${MONGO_AUTH_DATABASE}`;
}
