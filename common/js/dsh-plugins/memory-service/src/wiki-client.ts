/**
 * WikiClient — gRPC client for the game memory service's WikiService.
 *
 * The wiki sibling of {@link ./client.js MemoryClient}: the same
 * dominion-resolved target (`dominion:///game/memory:50051`), the same
 * proto-loader/credentials/round_robin channel construction and the same
 * injected-client DI seam (style/javascript.md §测试 — an injected grpc client
 * skips the resolver registration and the proto/fs read, so unit tests need no
 * module interception). All wiki orchestration (title normalization, exact
 * resolution, upsert, two-phase preflight/commit) lives in
 * `./wiki-operations.ts`; this client is a pass-through for the WikiService
 * RPCs (specs/069-wiki-template-web/contracts/dsh-plugins.md §3).
 *
 * A wiki's `name`/`wiki_id` never reaches model-visible text
 * (specs/068-wiki-round-pause/spec.md FR-009); the name is used here only to
 * address GetWiki/UpdateWiki/DeleteWiki.
 */

import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { registerDominionResolver } from "@dominion/common-js-grpc-resolver";

import {
  MEMORY_SERVICE_TARGET,
  buildChannelOptions,
  buildClientCredentials,
  callUnary,
  protoOptions,
  resolveProtoPath,
} from "./client.js";

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
 * never returned by search (specs/068-wiki-round-pause/spec.md FR-004).
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

/**
 * Client for the WikiService gRPC API.
 *
 * Registers the dominion resolver on construction, loads the game.proto
 * WikiService definition, and creates a service-specific gRPC client that
 * resolves the memory service endpoint (WikiService lives in the memory
 * service binary) via the dominion URI scheme. The optional `client` parameter
 * allows dependency injection of a mock client for testing without a live gRPC
 * connection (DI seam — no `vi.mock`).
 */
export class WikiClient implements WikiStore {
  private client: grpc.Client;

  /**
   * @param client Optional pre-configured gRPC client (for testing). An
   *   injected client already owns its channel, so it neither needs the
   *   dominion URI resolver nor the proto-loader/fs side effects.
   */
  constructor(client?: grpc.Client) {
    if (client) {
      this.client = client;
    } else {
      registerDominionResolver();

      const packageDefinition = protoLoader.loadSync(
        resolveProtoPath(),
        protoOptions(),
      );
      const proto = grpc.loadPackageDefinition(
        packageDefinition,
      ) as Record<string, unknown>;
      const wikiSvc = (proto as any).projects.game.WikiService;
      this.client = new wikiSvc(
        MEMORY_SERVICE_TARGET,
        buildClientCredentials(),
        buildChannelOptions(),
      );
    }
  }

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
    const request: {
      parent: string;
      query: string;
      pageToken?: string;
      pageSize?: number;
    } = {
      parent: `templates/${template}`,
      query,
      pageToken: options?.pageToken || undefined,
    };
    if (options?.pageSize !== undefined) {
      request.pageSize = options.pageSize;
    }
    const response = await this.call<{
      wikis: ListedWikiSummary[];
      nextPageToken?: string;
    }>((client, metadata, callOptions, cb) =>
      (client as any).searchWikis(request, metadata, callOptions, cb),
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
    const response = await this.call<ListedWiki>(
      (client, metadata, options, cb) =>
        (client as any).getWiki({ name }, metadata, options, cb),
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
    await this.call<void>((client, metadata, options, cb) =>
      (client as any).createWiki(
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
    await this.call<void>((client, metadata, options, cb) =>
      (client as any).updateWiki(
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
    await this.call<void>((client, metadata, options, cb) =>
      (client as any).deleteWiki(
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
    const request: {
      parent: string;
      pageToken?: string;
      pageSize?: number;
    } = {
      parent: `templates/${template}`,
      pageToken: options?.pageToken || undefined,
    };
    if (options?.pageSize !== undefined) {
      request.pageSize = options.pageSize;
    }
    const response = await this.call<{
      wikis: ListedWiki[];
      nextPageToken?: string;
    }>((client, metadata, callOptions, cb) =>
      (client as any).listWikis(request, metadata, callOptions, cb),
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

  /** Close the underlying gRPC client connection. */
  close(): void {
    this.client.close();
  }

  /** Shared unary invocation over this client's channel (see {@link callUnary}). */
  private call<T>(
    invoke: (
      client: grpc.Client,
      metadata: grpc.Metadata,
      options: { deadline: Date },
      callback: (err: grpc.ServiceError | null, response?: T) => void,
    ) => void,
  ): Promise<T> {
    return callUnary(this.client, "wiki client call", invoke);
  }
}
