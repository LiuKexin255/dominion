/**
 * The two prompt sections the memory plugin row contributes alongside its
 * tools: `memory:guidance` (the division of labour between the memory tool
 * and the knowledge base) and `wiki:guidance` (the cross-tool workflow of the
 * three knowledge-base tools). Both are English, markdown-sectioned static
 * text and each owns exactly one concern — the division section never
 * describes tool usage/parameters, the workflow section never restates the
 * division (specs/068-wiki-round-pause/spec.md FR-013;
 * specs/068-wiki-round-pause/contracts/dsh-plugins.md §2 items 6-7, §3).
 *
 * Neither section introduces third-party memory-framework terminology, and
 * neither injects knowledge-base entry content into the prompt — the wiki
 * store stays out-of-context by design
 * (specs/068-wiki-round-pause/spec.md Edge Cases「wiki 不注入快照」).
 *
 * The orders sit in the tool-guidance band (100–199), after the persona slot
 * (0), the team band (1–49) and the game section (`saolei:game` at 50), and
 * before the trailing data layer (200+)
 * (specs/060-agent-v2-team-optimize/contracts/prompt-sections.md §1). Within
 * the guidance band `saolei:guidance` owns 100, these two take 110/120, and
 * 130–199 stays free for later guidance sections. `memory:snapshot` itself is
 * owned by the host service
 * (`common/js/dsh-plugins/memory-service/src/snapshot.ts`).
 */

/** Section name for the memory-vs-wiki division of labour. */
export const MEMORY_GUIDANCE_SECTION_NAME = "memory:guidance";

/**
 * Order for the division section: the first guidance slot this plugin
 * contributes, after `saolei:guidance` (100).
 */
export const MEMORY_GUIDANCE_SECTION_ORDER = 110;

/** Section name for the knowledge-base cross-tool workflow. */
export const WIKI_GUIDANCE_SECTION_NAME = "wiki:guidance";

/** Order for the workflow section: after the division section. */
export const WIKI_GUIDANCE_SECTION_ORDER = 120;

/**
 * The memory-vs-wiki division of labour: what each store is for and how it
 * reaches the context. Deliberately silent on how to call either tool — that
 * belongs to the tool descriptions and {@link WIKI_GUIDANCE}
 * (specs/068-wiki-round-pause/spec.md FR-013).
 */
export const MEMORY_GUIDANCE = `## Memory and the knowledge base

Two stores hold information across turns, and they serve different roles:

- **Memory** is a small set of recent working notes. It is always visible in your system prompt, but only as a recency-bounded snapshot: the 10 most recently updated entries. It is the place for short, immediately relevant facts.
- **The knowledge base** holds large, detailed knowledge. It never enters your system prompt on its own and is read only on demand, so it can grow without crowding your context. It is the place for knowledge worth keeping beyond the current moment.

Prefer memory for what you need on every turn; prefer the knowledge base for what you need only occasionally and in depth.`;

/**
 * The knowledge-base cross-tool workflow: how `wiki_search`, `wiki_get` and
 * `wiki_write` cooperate. Deliberately silent on the memory-vs-wiki division,
 * which {@link MEMORY_GUIDANCE} owns
 * (specs/068-wiki-round-pause/spec.md FR-013).
 */
export const WIKI_GUIDANCE = `## Knowledge-base workflow

The knowledge-base tools form one discover → retrieve → persist flow:

- \`wiki_search\` — find out what exists: it matches titles and returns the matching titles, so start here when you are unsure whether relevant knowledge is already stored.
- \`wiki_get\` — retrieve entries by exact title and return their full content.
- \`wiki_write\` — persist knowledge by title. Writing is an upsert: a new title is created, an existing title has its content overwritten, so you never need to choose between "create" and "update". A batch write is all-or-nothing: if any entry is invalid, none is written.

Search first, retrieve only what you need, and write back knowledge that is worth reusing.`;
