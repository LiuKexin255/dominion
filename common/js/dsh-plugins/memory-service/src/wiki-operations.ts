/**
 * Wiki tool operations — the knowledge-base core behind the three wiki tools
 * (`wiki_search`/`wiki_get`/`wiki_write`). Semantics:
 *
 * - Title addressing only: both the stored title and the request title are
 *   normalized (NFKC + lower-case, the JS twin of the service's
 *   `NormalizeTitle` — specs/068-wiki-round-pause/data-model.md §2.1), so
 *   `wiki_get`/`wiki_write` resolve an entry by exact normalized title through
 *   SearchWikis. A wiki's resource `name`/`wiki_id` never reaches the result
 *   text (specs/068-wiki-round-pause/spec.md FR-009).
 * - `wiki_search` folds `next_page_token` with `page_size` 100 to return every
 *   matching title; content never enters its result
 *   (specs/068-wiki-round-pause/contracts/dsh-plugins.md §2 item 2).
 * - `wiki_write` is an upsert by normalized title (existing entry → UpdateWiki;
 *   absent → CreateWiki with `wiki_id = sha256(title_norm)[:32]` —
 *   specs/068-wiki-round-pause/data-model.md §5) and is batch-atomic: the
 *   preflight validates every entry (non-empty title/content, title ≤ 128
 *   runes, content ≤ 64KB) and resolves every update target BEFORE the commit
 *   phase runs, so one invalid entry writes nothing (the two-phase shape of
 *   `operations.ts` `applyBatch`;
 *   specs/068-wiki-round-pause/spec.md US2 scenario 4).
 * - Domain outcomes (empty query, no hit, missing titles, invalid batch) are
 *   TEXT results; only infrastructure failures propagate to the tool surface,
 *   which maps them to `wiki failed: …`
 *   (specs/068-wiki-round-pause/spec.md FR-011).
 */

import { createHash } from "node:crypto";

import { parseWikiName } from "./wiki-client.js";
import type { WikiStore, WikiSummary } from "./wiki-client.js";

/**
 * The `page_size` the search folding uses: the service maximum
 * (`projects/game/memory/handler/wiki_handler.go` `wikiPageSize` caps at the
 * domain maximum of 100, `specs/068-wiki-round-pause/contracts/wiki-service.md`
 * §4). A session's wiki set is bounded, so this reaches the end of the result
 * in one or two round trips.
 */
export const WIKI_SEARCH_PAGE_SIZE = 100;

/** Title upper bound in runes (service `ValidateWikiTitle`, FR-005). */
export const WIKI_TITLE_MAX_RUNES = 128;

/**
 * Content upper bound in bytes (service `ValidateWikiContent`, FR-005). The
 * tool preflight enforces it locally so an over-limit batch entry aborts the
 * whole batch before any write (US2 scenario 4) instead of failing mid-commit.
 */
export const WIKI_CONTENT_MAX_BYTES = 64 * 1024;

/** One `wiki_write` entry pair. */
export interface WikiWriteEntry {
  readonly title?: string;
  readonly content?: string;
}

/**
 * Arguments accepted by `wiki_write`: the single-entry form (`title` +
 * `content`) XOR the batch form (`entries`). The combination is a runtime
 * contract answered with text.
 */
export interface WikiWriteArgs {
  readonly title?: string;
  readonly content?: string;
  readonly entries?: readonly WikiWriteEntry[];
}

/** Domain-result texts (model-visible; never an internal id). */
export const WIKI_SEARCH_EMPTY_QUERY_TEXT =
  "wiki: query cannot be empty. Provide a keyword or phrase to search titles.";
export const WIKI_GET_EMPTY_TITLES_TEXT =
  "wiki: titles cannot be empty. Provide one or more titles to retrieve.";
export const WIKI_WRITE_AMBIGUOUS_ARGS_TEXT =
  "wiki: provide EITHER title/content (single entry) OR entries (batch), not both.";
export const WIKI_WRITE_MISSING_ARGS_TEXT =
  "wiki: provide EITHER title/content (single entry) OR entries (batch).";
export const WIKI_WRITE_EMPTY_ENTRIES_TEXT =
  "wiki: entries cannot be empty. Provide title/content (single entry) or a " +
  "non-empty entries list (batch).";

/**
 * Normalize a title exactly like the service's `NormalizeTitle`: NFKC Unicode
 * normalization then lower-casing (specs/068-wiki-round-pause/data-model.md
 * §2.1). The two implementations are kept in lockstep by the shared case table
 * in the tests.
 */
export function normalizeWikiTitle(title: string): string {
  return title.normalize("NFKC").toLowerCase();
}

/**
 * Generate the agent-side wiki id for a create: the first 32 hex characters
 * of `sha256(title_norm)` (specs/068-wiki-round-pause/data-model.md §5). The
 * charset `[a-f0-9]` satisfies the service's `[a-z0-9_-]+` wiki_id rule; the
 * id is deterministic, so the create branch of an upsert is idempotent across
 * a delete/recreate (and a concurrent race resolves to the same name).
 */
export function generateWikiId(titleNorm: string): string {
  return createHash("sha256").update(titleNorm).digest("hex").slice(0, 32);
}

/**
 * Read every SearchWikis page for `query` (folding `next_page_token` with
 * `page_size` = {@link WIKI_SEARCH_PAGE_SIZE}) and return the hits in service
 * order. Entries are deduplicated by resource name: offset pagination can
 * re-surface an entry when a write lands between pages, and the tool result
 * should list a title once.
 */
async function searchAll(
  store: WikiStore,
  template: string,
  session: string,
  query: string,
): Promise<WikiSummary[]> {
  const hits: WikiSummary[] = [];
  const seen = new Set<string>();
  let pageToken: string | undefined;
  do {
    const page = await store.searchWikis(template, session, query, {
      pageSize: WIKI_SEARCH_PAGE_SIZE,
      pageToken,
    });
    for (const wiki of page.wikis) {
      if (seen.has(wiki.name)) {
        continue;
      }
      seen.add(wiki.name);
      hits.push(wiki);
    }
    pageToken = page.nextPageToken.length > 0 ? page.nextPageToken : undefined;
  } while (pageToken !== undefined);
  return hits;
}

/**
 * Resolve one title to its exact entry: search by the normalized title and
 * keep the hit whose normalized title equals the request's. Uniqueness of
 * `title_norm` in the scope guarantees at most one match
 * (specs/068-wiki-round-pause/spec.md FR-002).
 */
async function resolveWiki(
  store: WikiStore,
  template: string,
  session: string,
  title: string,
): Promise<WikiSummary | undefined> {
  const norm = normalizeWikiTitle(title);
  const hits = await searchAll(store, template, session, norm);
  return hits.find((hit) => normalizeWikiTitle(hit.title) === norm);
}

/**
 * Run `wiki_search`: fold all search pages and render the matching titles (one
 * per line). An empty query and an empty result are text guidance, never an
 * error.
 */
export async function wikiSearch(
  store: WikiStore,
  template: string,
  session: string,
  query: string,
): Promise<string> {
  const trimmed = query.trim();
  if (trimmed.length === 0) {
    return WIKI_SEARCH_EMPTY_QUERY_TEXT;
  }
  const hits = await searchAll(store, template, session, trimmed);
  if (hits.length === 0) {
    return (
      `wiki: no titles matched '${trimmed}'. ` +
      "Try a different or shorter keyword, or a different term for the same idea."
    );
  }
  const lines = hits.map((hit) => `- ${hit.title}`);
  return [`wiki: ${hits.length} title(s) matched '${trimmed}':`, ...lines].join(
    "\n",
  );
}

/**
 * Run `wiki_get`: resolve each requested title by exact normalized title,
 * fetch its content, and report the titles that did not resolve with guidance
 * to search again. Hits are never discarded because a sibling title missed
 * (specs/068-wiki-round-pause/spec.md FR-008).
 */
export async function wikiGet(
  store: WikiStore,
  template: string,
  session: string,
  titles: readonly string[],
): Promise<string> {
  if (titles.length === 0) {
    return WIKI_GET_EMPTY_TITLES_TEXT;
  }
  const found: string[] = [];
  const missing: string[] = [];
  for (const raw of titles) {
    const title = raw.trim();
    if (title.length === 0) {
      // List the trimmed form, matching the other miss paths (a whitespace-only
      // request must not surface as `not found: '  '`).
      missing.push(title);
      continue;
    }
    const hit = await resolveWiki(store, template, session, title);
    if (hit === undefined) {
      missing.push(title);
      continue;
    }
    const wiki = await store.getWiki(template, session, parseWikiName(hit.name));
    found.push(`## ${wiki.title}\n${wiki.content}`);
  }

  const parts: string[] = [];
  if (found.length > 0) {
    parts.push(`wiki: ${found.length} entry(ies) retrieved.`);
    parts.push(...found);
  }
  if (missing.length > 0) {
    const listed = missing.map((title) => `'${title}'`).join(", ");
    parts.push(
      `wiki: ${missing.length} title(s) not found: ${listed}. ` +
        "Run wiki_search with a keyword to find the exact title, then retry wiki_get.",
    );
  }
  return parts.join("\n\n");
}

/** One preflighted write: a create (new id) or an update (existing id). */
interface PlannedWikiWrite {
  readonly kind: "create" | "update";
  readonly id: string;
  readonly title: string;
  readonly content: string;
}

/** Normalize the two accepted `wiki_write` forms into one entry list. */
function normalizeWriteEntries(args: WikiWriteArgs): WikiWriteEntry[] {
  if (args.entries != null) {
    return [...args.entries];
  }
  return [{ title: args.title, content: args.content }];
}

/**
 * Run `wiki_write`: upsert every entry by normalized title, atomically. The
 * preflight validates every entry and resolves every update target against the
 * store first; only when all entries are clean does the commit phase run the
 * RPCs in order (the two-phase shape of `operations.ts` `applyBatch`). A
 * failing entry aborts the batch with an explanatory text and ZERO writes
 * (specs/068-wiki-round-pause/spec.md FR-007/US2 scenario 4); an
 * infrastructure failure mid-commit propagates to the tool surface as
 * `wiki failed: …`, with earlier writes possibly applied (no service
 * transaction API — the same limitation as the memory batch).
 */
export async function wikiWrite(
  store: WikiStore,
  template: string,
  session: string,
  args: WikiWriteArgs,
): Promise<string> {
  const hasSingle = args.title != null || args.content != null;
  const hasBatch = args.entries != null;
  if (hasSingle && hasBatch) {
    return WIKI_WRITE_AMBIGUOUS_ARGS_TEXT;
  }
  if (!hasSingle && !hasBatch) {
    return WIKI_WRITE_MISSING_ARGS_TEXT;
  }
  if (hasBatch && args.entries?.length === 0) {
    return WIKI_WRITE_EMPTY_ENTRIES_TEXT;
  }

  const entries = normalizeWriteEntries(args);
  // A batch failure names the failing entry and states the all-or-nothing
  // rule; the single form has no batch to abort, so it reports only the
  // reason (the neutral style of the memory tool's single-form texts).
  const failureText = (pos: string, reason: string): string =>
    hasBatch
      ? `wiki: ${pos} failed: ${reason}. No entries were written ` +
        "(a batch is all-or-nothing)."
      : `wiki: ${reason}. Nothing was written.`;

  // Preflight: validate each entry and resolve its target. `planned` tracks
  // the ids chosen earlier in THIS batch so a repeated title (or one whose
  // normalized form repeats) updates the planned entry instead of racing a
  // second create against the same deterministic id.
  const writes: PlannedWikiWrite[] = [];
  const planned = new Map<string, string>();
  for (let i = 0; i < entries.length; i += 1) {
    const entry = entries[i];
    const pos = `entry ${i + 1}`;
    const title = (entry.title ?? "").trim();
    const content = entry.content ?? "";
    if (title.length === 0) {
      return failureText(pos, "title is required");
    }
    if ([...title].length > WIKI_TITLE_MAX_RUNES) {
      return failureText(
        pos,
        `title exceeds ${WIKI_TITLE_MAX_RUNES} characters`,
      );
    }
    if (content.trim().length === 0) {
      return failureText(pos, "content is required");
    }
    if (Buffer.byteLength(content, "utf8") > WIKI_CONTENT_MAX_BYTES) {
      return failureText(
        pos,
        `content exceeds ${WIKI_CONTENT_MAX_BYTES} bytes`,
      );
    }

    const norm = normalizeWikiTitle(title);
    const plannedId = planned.get(norm);
    if (plannedId !== undefined) {
      writes.push({ kind: "update", id: plannedId, title, content });
      continue;
    }
    const hit = await resolveWiki(store, template, session, title);
    let id: string;
    let kind: "create" | "update";
    if (hit === undefined) {
      id = generateWikiId(norm);
      kind = "create";
    } else {
      id = parseWikiName(hit.name);
      kind = "update";
    }
    planned.set(norm, id);
    writes.push({ kind, id, title, content });
  }

  // Commit — every preflight check passed.
  for (const write of writes) {
    if (write.kind === "create") {
      await store.createWiki(
        template,
        session,
        write.id,
        write.title,
        write.content,
      );
    } else {
      await store.updateWiki(template, session, write.id, write.content);
    }
  }

  const lines = writes.map(
    (write) => `${write.kind === "create" ? "created" : "updated"}: '${write.title}'`,
  );
  return [`wiki: ${writes.length} entry(ies) applied`, ...lines].join("\n");
}
