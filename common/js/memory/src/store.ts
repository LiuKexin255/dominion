/**
 * Store adapters — the domain-level seam over the injected gRPC service
 * clients (MemoryService and WikiService).
 *
 * Connection infrastructure (resolver, proto loading, credentials, channel
 * construction and the channel policy) is owned by the composition-level
 * gRPC runtime (specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md), and so
 * is the call-local unary policy (`waitForReady` + deadline + pre-call probe +
 * failure logging): it reaches these adapters as the injected
 * {@link UnaryPolicy}, whose only implementation lives in
 * `common/js/dsh-plugins/grpc-runtime/src/unary.ts`
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1 义务 2).
 * This module keeps only the client adaptation: resource-name construction,
 * the AIP request shapes and page walking. The constructor takes the injected
 * client and policy, so unit tests pass `vi.fn()`-backed doubles with no
 * module interception (style/javascript.md §测试).
 *
 * The two service clients are consumed through the structural call surfaces
 * {@link MemoryServiceCalls}/{@link WikiServiceCalls}: the caller builds them
 * with `ctx.grpc.client<MemoryServiceCalls>(...)`, whose single `as T` cast is
 * the repository's proto-object cast point (specs/074-dsh-grpc-runtime/
 * spec.md FR-012). This module MUST NOT
 * import generated proto code or the saolei-loop orchestration package
 * (specs/074-dsh-grpc-runtime/contracts/memory-library.md §3).
 *
 * The storage API follows
 * `specs/071-memory-preset-scope/contracts/dsh-plugins.md` §2
 * (https://google.aip.dev/133, https://google.aip.dev/134): a pass-through
 * client for the memory RPCs over the resource pattern
 * `templates/{template}/presets/{preset}/memories/{memory}`; all
 * hermes-style conversion lives in `operations.ts`. The wiki sibling is
 * addressed by `templates/{template}/wikis/{wikiId}`
 * (specs/069-wiki-template-web/contracts/wiki-service.md §1). A resource
 * `memory_id`/`wiki_id` is used for internal location only and MUST NOT be
 * rendered into model-visible text (specs/059-agent-v2-team-mode/spec.md
 * FR-007; specs/069-wiki-template-web/contracts/dsh-plugins.md §3).
 */

import * as grpc from "@grpc/grpc-js";

// ── MemoryService call surface ────────────────────────────────────────────

/**
 * One memory entry as returned by `listMemories`: the service's internal
 * resource id (`memory_id`) and the entry text. The id is used only for
 * internal location (replace/remove old_text matching, snapshot rendering
 * excludes it) and MUST NOT be rendered into LLM-visible text
 * (specs/059-agent-v2-team-mode/spec.md FR-007). It is derived JS-side from
 * the resource name through {@link parseMemoryName}: the Memory resource
 * carries no id field (specs/068-wiki-round-pause/spec.md FR-017).
 */
export interface MemoryEntry {
  memory_id: string;
  content: string;
}

/**
 * Build a Memory resource name per AIP-122:
 * `templates/{template}/presets/{preset}/memories/{memoryId}`. The parent is
 * the materialization source preset (specs/071-memory-preset-scope/contracts/
 * dsh-plugins.md §1/§2).
 */
export function memoryName(
  template: string,
  preset: string,
  memoryId: string,
): string {
  return `templates/${template}/presets/${preset}/memories/${memoryId}`;
}

/**
 * Extract the `{memory}` terminal segment of a Memory resource name
 * (`templates/{template}/presets/{preset}/memories/{memory}`) — the
 * symmetric inverse of {@link memoryName}. The resource message carries no
 * separate id field, so the id is derived from the name here (FR-017,
 * specs/068-wiki-round-pause/contracts/memory-resource-identity.md §1).
 */
export function parseMemoryName(name: string): string {
  return name.slice(name.lastIndexOf("/") + 1);
}

/**
 * One decoded `ListMemoriesResponse.memories[]` wire entry (`longs: String`).
 * The Memory resource has no `memory_id` field
 * (specs/068-wiki-round-pause/spec.md FR-017): the id is derived from `name`
 * through {@link parseMemoryName}.
 */
interface ListedMemory {
  name?: string;
  content?: string;
}

/**
 * Options for {@link MemoryStoreRpcs.listMemories}. `orderBy` is forwarded to
 * the wire as the AIP-132 `order_by` value; `pageSize` switches the call to a
 * single-page read (see {@link MemoryStoreRpcs.listMemories}).
 */
export interface ListMemoriesOptions {
  /** AIP-132 order_by value; an undefined value is not sent. */
  orderBy?: string;
  /** Page size for a single request; an undefined value is not sent. */
  pageSize?: number;
}

interface CreateMemoryRequest {
  parent: string;
  memoryId: string;
  memory: { name: string; content: string };
}

interface UpdateMemoryRequest {
  memory: { name: string; content: string };
  updateMask: { paths: readonly string[] };
}

interface DeleteMemoryRequest {
  name: string;
}

interface ListMemoriesRequest {
  parent: string;
  pageToken?: string;
  pageSize?: number;
  orderBy?: string;
}

interface ListMemoriesResponse {
  memories: ListedMemory[];
  nextPageToken?: string;
}

/**
 * The storage seam the memory operations consume: exactly the four
 * MemoryService RPCs the domain needs (survey/deepseek-harness-memory-plugin.md
 * §7 option A — the whole storage dependency collapses onto this interface,
 * implemented by {@link MemoryClient}). Tests inject a `vi.fn()`-backed
 * double instead of a live gRPC client.
 */
export interface MemoryStoreRpcs {
  createMemory(
    template: string,
    preset: string,
    memoryId: string,
    content: string,
  ): Promise<void>;
  updateMemory(
    template: string,
    preset: string,
    memoryId: string,
    content: string,
  ): Promise<void>;
  deleteMemory(template: string, preset: string, memoryId: string): Promise<void>;
  listMemories(
    template: string,
    preset: string,
    options?: ListMemoriesOptions,
  ): Promise<MemoryEntry[]>;
}

// ── WikiService call surface ──────────────────────────────────────────────

/**
 * Build a Wiki resource name per AIP-122:
 * `templates/{template}/wikis/{wikiId}`
 * (specs/069-wiki-template-web/contracts/wiki-service.md §1).
 */
export function wikiName(template: string, wikiId: string): string {
  return `templates/${template}/wikis/${wikiId}`;
}

/**
 * Extract the `{wiki}` terminal segment of a Wiki resource name
 * (`templates/{template}/wikis/{wiki}`) — the symmetric inverse of
 * {@link wikiName}. The resource message carries no separate id field, so the
 * id is derived from the name here
 * (specs/068-wiki-round-pause/spec.md FR-017).
 */
export function parseWikiName(name: string): string {
  return name.slice(name.lastIndexOf("/") + 1);
}

/**
 * One SearchWikis hit: the lightweight `{name, title}` pair. `content` is
 * never returned by search
 * (specs/069-wiki-template-web/contracts/wiki-service.md §2).
 */
export interface WikiSummary {
  readonly name: string;
  readonly title: string;
}

/** One wiki entry with the full body, as returned by GetWiki/ListWikis. */
export interface Wiki {
  readonly name: string;
  readonly title: string;
  readonly content: string;
}

/** Options for {@link WikiStore.searchWikis}; undefined fields are not sent. */
export interface SearchWikisOptions {
  /** AIP-158 page size; an undefined value is not sent. */
  pageSize?: number;
  /** AIP-158 page token from a previous call; an undefined value is not sent. */
  pageToken?: string;
}

/** One SearchWikis page: the hits plus the continuation token ("" = last). */
export interface SearchWikisPage {
  readonly wikis: WikiSummary[];
  readonly nextPageToken: string;
}

/** Options for {@link WikiStore.listWikis}; undefined fields are not sent. */
export interface ListWikisOptions {
  /** AIP-158 page size; an undefined value is not sent. */
  pageSize?: number;
  /** AIP-158 page token from a previous call; an undefined value is not sent. */
  pageToken?: string;
}

/** One ListWikis page: the full resources plus the continuation token. */
export interface ListWikisPage {
  readonly wikis: Wiki[];
  readonly nextPageToken: string;
}

/**
 * The fields {@link WikiStore.updateWiki} can change. Only `content` is
 * mutable through this seam: the wiki tool upserts by title and never
 * re-addresses an existing entry
 * (specs/068-wiki-round-pause/spec.md Assumptions).
 */
export interface WikiUpdate {
  readonly content: string;
}

/**
 * The storage seam the wiki operations consume
 * (specs/069-wiki-template-web/contracts/dsh-plugins.md §3). Every method
 * takes the template as its single scope parameter: a wiki collection is
 * shared by every session working with that template. Implemented by
 * {@link WikiClient}; tests inject a `vi.fn()`-backed double instead of a live
 * gRPC client.
 */
export interface WikiStore {
  searchWikis(
    template: string,
    query: string,
    options?: SearchWikisOptions,
  ): Promise<SearchWikisPage>;
  getWiki(template: string, wikiId: string): Promise<Wiki>;
  createWiki(
    template: string,
    wikiId: string,
    title: string,
    content: string,
  ): Promise<void>;
  updateWiki(
    template: string,
    wikiId: string,
    wiki: WikiUpdate,
  ): Promise<void>;
  deleteWiki(template: string, wikiId: string): Promise<void>;
  listWikis(
    template: string,
    options?: ListWikisOptions,
  ): Promise<ListWikisPage>;
}

/** One decoded `SearchWikisResponse.wikis[]` wire entry. */
interface ListedWikiSummary {
  name?: string;
  title?: string;
}

/** One decoded `Wiki` wire entry (only the fields the tools consume). */
interface ListedWiki {
  name?: string;
  title?: string;
  content?: string;
}

interface SearchWikisRequest {
  parent: string;
  query: string;
  pageToken?: string;
  pageSize?: number;
}

interface SearchWikisResponse {
  wikis: ListedWikiSummary[];
  nextPageToken?: string;
}

interface GetWikiRequest {
  name: string;
}

interface CreateWikiRequest {
  parent: string;
  wikiId: string;
  wiki: { name: string; title: string; content: string };
}

interface UpdateWikiRequest {
  wiki: { name: string; content: string };
  updateMask: { paths: readonly string[] };
}

interface DeleteWikiRequest {
  name: string;
}

interface ListWikisRequest {
  parent: string;
  pageToken?: string;
  pageSize?: number;
}

interface ListWikisResponse {
  wikis: ListedWiki[];
  nextPageToken?: string;
}

// ── Structured call surfaces (consumed by the injected clients) ───────────

/**
 * Callback-style unary RPC method signature: the shape every grpc-js service
 * client method has once the package definition is loaded. The caller
 * supplies it as the generic argument to `ctx.grpc.client<...>`.
 */
export type UnaryCall<Req, Res> = (
  request: Req,
  metadata: grpc.Metadata,
  options: grpc.CallOptions,
  callback: (err: grpc.ServiceError | null, response?: Res) => void,
) => void;

/** The MemoryService call surface (the four RPCs the domain consumes). */
export interface MemoryServiceCalls {
  createMemory: UnaryCall<CreateMemoryRequest, void>;
  updateMemory: UnaryCall<UpdateMemoryRequest, void>;
  deleteMemory: UnaryCall<DeleteMemoryRequest, void>;
  listMemories: UnaryCall<ListMemoriesRequest, ListMemoriesResponse>;
}

/** The WikiService call surface (the six RPCs the wiki seam exposes). */
export interface WikiServiceCalls {
  searchWikis: UnaryCall<SearchWikisRequest, SearchWikisResponse>;
  getWiki: UnaryCall<GetWikiRequest, ListedWiki>;
  createWiki: UnaryCall<CreateWikiRequest, void>;
  updateWiki: UnaryCall<UpdateWikiRequest, void>;
  deleteWiki: UnaryCall<DeleteWikiRequest, void>;
  listWikis: UnaryCall<ListWikisRequest, ListWikisResponse>;
}

/** The composed domain seam: memory storage plus the wiki store. */
export interface MemoryStore extends MemoryStoreRpcs, WikiStore {}

// ── Injected unary invocation policy ─────────────────────────────────────

/**
 * The minimal structural view of an injected gRPC service client as far as the
 * invocation policy is concerned: the policy only forwards the client it was
 * given (the adapters address their RPCs through the closure they already
 * hold), so the seam deliberately names no client member and imports no
 * `grpc.Client`. Declared as `object` rather than `unknown` so call sites stay
 * type-checked, and it accepts every grpc-js service client as prepared by
 * `ctx.grpc.client<...>`
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1 义务 2).
 */
export type UnaryClient = object;

/**
 * Callback-style unary invocation shape shared with the composition-level
 * runtime (`common/js/dsh-plugins/grpc-runtime/src/unary.ts`): the policy hands
 * its client to the invoke callback, and an adapter's callback ignores it
 * because it already holds the same client.
 */
export type UnaryInvoke<T> = (
  client: UnaryClient,
  metadata: grpc.Metadata,
  options: { deadline: Date },
  callback: (err: grpc.ServiceError | null, response?: T) => void,
) => void;

/**
 * The call-local unary invocation policy, structurally compatible with
 * `ctx.grpc.callUnary`
 * (`common/js/dsh-plugins/grpc-runtime/src/client.ts`;
 * specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md §2 O3). The strategy —
 * `waitForReady`, a 10s default deadline, a pre-call channel probe and the
 * post-failure channel-state log — has a single owner in the composition-level
 * runtime and reaches these adapters only through this injected seam; the
 * library never keeps a copy of it
 * (specs/076-agent-v2-optimize/contracts/memory-library-layering.md §1 义务 2;
 * specs/076-agent-v2-optimize/research.md D10). Tests inject a `vi.fn()`-backed
 * double of the same shape.
 */
export type UnaryPolicy = <T>(
  client: UnaryClient,
  label: string,
  invoke: UnaryInvoke<T>,
  deadlineMs?: number,
) => Promise<T>;

// ── Client adapters ───────────────────────────────────────────────────────

/**
 * Client for the MemoryService gRPC API over an injected service client.
 *
 * @param client The service client built by `ctx.grpc.client<MemoryServiceCalls>`
 *   (or a `vi.fn()`-backed double in tests). The connection is owned by the
 *   composition-level runtime, so this adapter never constructs one.
 * @param callUnary The injected unary policy every RPC goes through
 *   ({@link UnaryPolicy}; see the module header).
 */
export class MemoryClient implements MemoryStoreRpcs {
  constructor(
    private readonly client: MemoryServiceCalls,
    private readonly callUnary: UnaryPolicy,
  ) {}

  /**
   * Create a memory entry under a preset (AIP-133). The request carries the
   * resource EMBEDDED — `memory_id` (the service-internal storage key,
   * generated by the agent on add) plus the `Memory` body `{name, content}`.
   *
   * @throws {grpc.ServiceError} Propagates gRPC errors (ALREADY_EXISTS when
   *   the memory_id collides — callers dedupe first).
   */
  async createMemory(
    template: string,
    preset: string,
    memoryId: string,
    content: string,
  ): Promise<void> {
    const name = memoryName(template, preset, memoryId);
    await this.call<void>((_client, metadata, options, cb) =>
      this.client.createMemory(
        {
          parent: `templates/${template}/presets/${preset}`,
          memoryId,
          memory: { name, content },
        },
        metadata,
        options,
        cb,
      ),
    );
  }

  /**
   * Update a memory entry's content (AIP-134). The request is
   * `Memory{name, content}` + a `FieldMask` whose only path is `"content"` —
   * the sole mutable Memory field.
   *
   * @throws {grpc.ServiceError} NOT_FOUND when the memory does not exist.
   */
  async updateMemory(
    template: string,
    preset: string,
    memoryId: string,
    content: string,
  ): Promise<void> {
    await this.call<void>((_client, metadata, options, cb) =>
      this.client.updateMemory(
        {
          memory: {
            name: memoryName(template, preset, memoryId),
            content,
          },
          updateMask: { paths: ["content"] },
        },
        metadata,
        options,
        cb,
      ),
    );
  }

  /**
   * Delete a memory entry (AIP-135).
   *
   * @throws {grpc.ServiceError} NOT_FOUND when the memory does not exist.
   */
  async deleteMemory(
    template: string,
    preset: string,
    memoryId: string,
  ): Promise<void> {
    await this.call<void>((_client, metadata, options, cb) =>
      this.client.deleteMemory(
        { name: memoryName(template, preset, memoryId) },
        metadata,
        options,
        cb,
      ),
    );
  }

  /**
   * List memory entries under a preset.
   *
   * Without `options.pageSize` the call walks every page until
   * `next_page_token` is empty (the write path's full-collection semantics).
   * With `options.pageSize` the call issues exactly one request carrying
   * `pageSize`/`orderBy` (undefined fields are not sent) and returns that
   * first page without following `next_page_token` — page_size is the page
   * upper bound, and a single server-ordered page is all the snapshot loader
   * needs (specs/065-agent-v2-team-refine/contracts/memory-snapshot-recency.md
   * §2).
   *
   * @throws {grpc.ServiceError} Propagates gRPC errors from the service.
   */
  async listMemories(
    template: string,
    preset: string,
    options?: ListMemoriesOptions,
  ): Promise<MemoryEntry[]> {
    const parent = `templates/${template}/presets/${preset}`;
    const singlePage = options?.pageSize !== undefined;
    const entries: MemoryEntry[] = [];
    let pageToken = "";
    do {
      const request: ListMemoriesRequest = {
        parent,
        pageToken: pageToken || undefined,
      };
      if (options?.pageSize !== undefined) {
        request.pageSize = options.pageSize;
      }
      if (options?.orderBy !== undefined) {
        request.orderBy = options.orderBy;
      }
      const response = await this.call<ListMemoriesResponse | undefined>(
        (_client, metadata, callOptions, cb) =>
          this.client.listMemories(request, metadata, callOptions, cb),
      );
      for (const m of response?.memories ?? []) {
        if (m.name != null && m.content != null) {
          entries.push({
            memory_id: parseMemoryName(m.name),
            content: m.content,
          });
        }
      }
      pageToken = singlePage ? "" : (response?.nextPageToken ?? "");
    } while (pageToken.length > 0);
    return entries;
  }

  /**
   * Shared unary invocation through the injected policy (see
   * {@link UnaryPolicy}; the `label` keeps the memory-specific log wording).
   */
  private call<T>(invoke: UnaryInvoke<T>): Promise<T> {
    return this.callUnary(this.client, "memory client call", invoke);
  }
}

/**
 * Client for the WikiService gRPC API over an injected service client.
 *
 * @param client The service client built by `ctx.grpc.client<WikiServiceCalls>`
 *   (or a `vi.fn()`-backed double in tests). The connection is owned by the
 *   composition-level runtime, so this adapter never constructs one.
 * @param callUnary The injected unary policy every RPC goes through
 *   ({@link UnaryPolicy}; see the module header).
 */
export class WikiClient implements WikiStore {
  constructor(
    private readonly client: WikiServiceCalls,
    private readonly callUnary: UnaryPolicy,
  ) {}

  /**
   * Search the template's wikis by normalized title substring (AIP-136 custom
   * method). The query is sent as given — the service normalizes both sides
   * (NFKC + lower-case); page_size/page_token are forwarded when present.
   *
   * @throws {grpc.ServiceError} Propagates gRPC errors from the service.
   */
  async searchWikis(
    template: string,
    query: string,
    options?: SearchWikisOptions,
  ): Promise<SearchWikisPage> {
    const request: SearchWikisRequest = {
      parent: `templates/${template}`,
      query,
      pageToken: options?.pageToken || undefined,
    };
    if (options?.pageSize !== undefined) {
      request.pageSize = options.pageSize;
    }
    const response = await this.call<SearchWikisResponse | undefined>(
      (_client, metadata, callOptions, cb) =>
        this.client.searchWikis(request, metadata, callOptions, cb),
    );
    const wikis: WikiSummary[] = [];
    for (const summary of response?.wikis ?? []) {
      if (summary.name != null && summary.title != null) {
        wikis.push({ name: summary.name, title: summary.title });
      }
    }
    return { wikis, nextPageToken: response?.nextPageToken ?? "" };
  }

  /**
   * Get one wiki by name (AIP-131) — the only read path that returns content.
   *
   * @throws {grpc.ServiceError} NOT_FOUND when the wiki does not exist.
   */
  async getWiki(template: string, wikiId: string): Promise<Wiki> {
    const name = wikiName(template, wikiId);
    const response = await this.call<ListedWiki | undefined>(
      (_client, metadata, options, cb) =>
        this.client.getWiki({ name }, metadata, options, cb),
    );
    return {
      name: response?.name ?? name,
      title: response?.title ?? "",
      content: response?.content ?? "",
    };
  }

  /**
   * Create a wiki under a template (AIP-133). The request carries the resource
   * EMBEDDED (`wiki.title`/`wiki.content`) plus the agent-generated
   * `wiki_id`; the name is derived from the parent and that id.
   *
   * @throws {grpc.ServiceError} ALREADY_EXISTS when the template already has a
   *   title normalizing to this one.
   */
  async createWiki(
    template: string,
    wikiId: string,
    title: string,
    content: string,
  ): Promise<void> {
    await this.call<void>((_client, metadata, options, cb) =>
      this.client.createWiki(
        {
          parent: `templates/${template}`,
          wikiId,
          wiki: { name: wikiName(template, wikiId), title, content },
        },
        metadata,
        options,
        cb,
      ),
    );
  }

  /**
   * Update a wiki's content (AIP-134). The request is `Wiki{name, content}` +
   * a `FieldMask` whose only path is `"content"` — the wiki tool never changes
   * a title (specs/068-wiki-round-pause/spec.md Assumptions).
   *
   * @throws {grpc.ServiceError} NOT_FOUND when the wiki does not exist.
   */
  async updateWiki(
    template: string,
    wikiId: string,
    wiki: WikiUpdate,
  ): Promise<void> {
    await this.call<void>((_client, metadata, options, cb) =>
      this.client.updateWiki(
        {
          wiki: { name: wikiName(template, wikiId), content: wiki.content },
          updateMask: { paths: ["content"] },
        },
        metadata,
        options,
        cb,
      ),
    );
  }

  /**
   * Delete a wiki by name (AIP-135).
   *
   * @throws {grpc.ServiceError} NOT_FOUND when the wiki does not exist.
   */
  async deleteWiki(template: string, wikiId: string): Promise<void> {
    await this.call<void>((_client, metadata, options, cb) =>
      this.client.deleteWiki(
        { name: wikiName(template, wikiId) },
        metadata,
        options,
        cb,
      ),
    );
  }

  /**
   * List the template's wikis (AIP-132; full resources, content included).
   * page_size/page_token are forwarded when present; the caller owns page
   * walking.
   *
   * @throws {grpc.ServiceError} Propagates gRPC errors from the service.
   */
  async listWikis(
    template: string,
    options?: ListWikisOptions,
  ): Promise<ListWikisPage> {
    const request: ListWikisRequest = {
      parent: `templates/${template}`,
      pageToken: options?.pageToken || undefined,
    };
    if (options?.pageSize !== undefined) {
      request.pageSize = options.pageSize;
    }
    const response = await this.call<ListWikisResponse | undefined>(
      (_client, metadata, callOptions, cb) =>
        this.client.listWikis(request, metadata, callOptions, cb),
    );
    const wikis: Wiki[] = [];
    for (const wiki of response?.wikis ?? []) {
      if (wiki.name != null && wiki.title != null) {
        wikis.push({
          name: wiki.name,
          title: wiki.title,
          content: wiki.content ?? "",
        });
      }
    }
    return { wikis, nextPageToken: response?.nextPageToken ?? "" };
  }

  /**
   * Shared unary invocation through the injected policy (see
   * {@link UnaryPolicy}).
   */
  private call<T>(invoke: UnaryInvoke<T>): Promise<T> {
    return this.callUnary(this.client, "wiki client call", invoke);
  }
}
