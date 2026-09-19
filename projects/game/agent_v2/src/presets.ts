/**
 * presets.ts — the agent_v2 side of the preset persistence wiring: the
 * deployment Mongo connection and credential derivation the preset-authoring
 * plugin's Mongo store consumes (specs/059-agent-v2-team-mode/tasks.md: "收缩为
 * Mongo 连接/凭据供 authoring Store 复用"). The generic plugin carries no
 * Dominion deployment logic; the bootstrap resolves the credentialed URI here
 * and injects it into the composition row via the `MONGO_URI` environment
 * variable — the established host-side injection pattern (GLM_BASE_URL/
 * GLM_API_KEY, projects/game/agent_v2/src/dsh.ts).
 *
 * The deployment lookup (target parsing, endpoint resolution, same-source
 * credential derivation) is the shared `@dominion/common-js-mongo` library
 * (specs/067-redis-infra-support/contracts/common-db-libs.md §3/§4); a direct
 * `MONGO_URI` still takes precedence for tests/local.
 */

import { resolveMongoUri as resolveTargetMongoUri } from "@dominion/common-js-mongo";
import type { MongoClientOptions } from "@dominion/common-js-mongo";

/** The agent_v2 service's own Mongo database (style/mongo.md isolation). */
export const PRESET_DATABASE = "game_agent_v2";

/** The preset collection inside {@link PRESET_DATABASE}. */
export const PRESET_COLLECTION_NAME = "presets";

/** Logical Dominion target backing agent_v2's preset storage. */
export const MONGO_TARGET = "dominion:///game/mongo:27017";

/**
 * Endpoint precedence: `MONGO_URI` as-is (tests/local direct connect) > the
 * Dominion resolver answer for {@link MONGO_TARGET}, turned into a credentialed
 * `mongodb://` URI (the derived credential is same-source with the Go
 * services'). Injectable for tests (style/javascript.md Mock convention).
 */
export async function resolveMongoUri(
  deps: MongoClientOptions = {},
): Promise<string> {
  const env = deps.env ?? process.env;
  const direct = env.MONGO_URI;
  if (direct) {
    return direct;
  }
  return resolveTargetMongoUri(MONGO_TARGET, deps);
}
