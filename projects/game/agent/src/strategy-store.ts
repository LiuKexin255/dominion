/**
 * strategy-store.ts — Long-term strategy memory for the saolei team graph.
 *
 * The strategy (长期策略记忆) is shared between the `player` and `planner`
 * agents, keyed by session id (FR-013 / research.md D4). It is persisted by
 * the agent service itself directly to MongoDB (the current mongo instance,
 * `strategies` collection) — NOT via the prompt service (D4 revision #5:
 * the prompt service only manages TeamProfile static configuration).
 *
 * Contract: `specs/031-team-template-mode/contracts/strategy-store-contract.md`.
 * The team graph (player/planner nodes, `update_strategy` tool) depends on the
 * `StrategyStore` interface (DI) so tests inject the in-memory fake and the
 * production wiring (server.ts, Phase 5 T021) injects the mongo-backed impl.
 */

/**
 * Strategy long-term memory storage interface (team graph dependency, DI;
 * testable with a fake).
 */
export interface StrategyStore {
	/**
	 * Read the strategy for a session; returns the empty string `""` when no
	 * record exists (需求方 #3 — there is no "preset strategy"; the content
	 * is first written by the planner's `update_strategy`).
	 */
	get(sessionId: string): Promise<string>;
	/** Write/update the strategy for a session (called by `update_strategy`). */
	put(sessionId: string, content: string): Promise<void>;
}

/**
 * MongoDB document shape for the `strategies` collection
 * (`specs/031-team-template-mode/contracts/strategy-store-contract.md` §3).
 */
export interface StrategyDocument {
	session_id: string;
	content: string;
	create_time: Date;
	update_time: Date;
}

/** MongoDB `strategies` collection name (contract §3). */
export const STRATEGIES_COLLECTION = "strategies";

/**
 * The narrow set of `mongodb.Collection<StrategyDocument>` operations
 * `MongoStrategyStore` needs. Declared as an interface so tests inject a fake
 * collection (no real mongo round-trip) while the production wiring passes a
 * real driver `Collection` (structurally compatible — see
 * `style/javascript.md` §测试 — DI over vi.mock).
 */
export interface StrategyCollection {
	findOne(
		filter: { session_id: string },
	): Promise<StrategyDocument | null>;
	updateOne(
		filter: { session_id: string },
		update: {
			$set: {
				session_id: string;
				content: string;
				update_time: Date;
			};
			$setOnInsert: { create_time: Date };
		},
		options: { upsert: true },
	): Promise<unknown>;
	createIndex(
		index: { session_id: 1 },
		options: { unique: true },
	): Promise<string>;
}

/**
 * Mongo-backed `StrategyStore` (agent mongo-backed memory store, D4).
 *
 * `get` reads by `session_id` and returns `""` for a missing document;
 * `put` upserts by `session_id` (updates `content` + `update_time`, inserts
 * with `create_time` on first write — contract §3).
 *
 * The driver `Collection` is injected: the constructor takes a
 * `StrategyCollection` (a structural subset of `mongodb.Collection`), so the
 * upsert/index logic is unit-testable with a fake collection and the real
 * client wiring stays in server.ts (Phase 5 T021 — mongo client resolved from
 * the `game/mongo` target, connection config via secrets, class-similar to
 * the prompt service's connection approach).
 *
 * @param collection The `strategies` collection (or a test fake).
 */
export class MongoStrategyStore implements StrategyStore {
	constructor(private readonly collection: StrategyCollection) {}

	async get(sessionId: string): Promise<string> {
		const doc = await this.collection.findOne({ session_id: sessionId });
		return doc ? doc.content : "";
	}

	async put(sessionId: string, content: string): Promise<void> {
		const now = new Date();
		await this.collection.updateOne(
			{ session_id: sessionId },
			{
				$set: {
					session_id: sessionId,
					content,
					update_time: now,
				},
				// create_time is set on insert only — an existing document's
				// creation time is never clobbered by a later upsert.
				$setOnInsert: { create_time: now },
			},
			{ upsert: true },
		);
	}

	/**
	 * Create the unique index on `session_id` (contract §3: unique index; the
	 * key is the strategy namespace = session id, FR-013). Called once at
	 * wiring time (server.ts, T021); idempotent in mongo.
	 */
	async ensureIndexes(): Promise<void> {
		await this.collection.createIndex(
			{ session_id: 1 },
			{ unique: true },
		);
	}
}

/**
 * In-memory `StrategyStore` fake (tests). A plain `Map` keyed by session id;
 * `get` on a missing key returns `""` (same contract as the mongo impl).
 */
export class FakeStrategyStore implements StrategyStore {
	private readonly map = new Map<string, string>();

	async get(sessionId: string): Promise<string> {
		return this.map.get(sessionId) ?? "";
	}

	async put(sessionId: string, content: string): Promise<void> {
		this.map.set(sessionId, content);
	}
}
