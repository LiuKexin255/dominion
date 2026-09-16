/**
 * MemoryClient — gRPC client for the game memory service (MemoryService).
 *
 * The storage API follows
 * `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md` §3
 * (https://google.aip.dev/133, https://google.aip.dev/134): a pass-through
 * client for the four RPCs over the resource pattern
 * `templates/{template}/sessions/{session}/memories/{memory}`. All
 * hermes-style conversion (old_text location, memory_id generation) lives in
 * `operations.ts`; this client never renders a `memory_id` into model-visible
 * text (specs/059-agent-v2-team-mode/spec.md FR-007;
 * specs/064-memory-split-fold-remain/contracts/dsh-plugins.md §2).
 *
 * The optional `client` constructor parameter is the DI seam
 * (style/javascript.md §测试): an injected grpc client skips the dominion
 * resolver registration and the proto-loader filesystem read, so unit tests
 * run without a live channel — no module interception.
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { registerDominionResolver } from "@dominion/common-js-grpc-resolver";
import { error, info } from "@dominion/common-js-logs";

/** Dominion resolver target for the memory service. */
export const MEMORY_SERVICE_TARGET = "dominion:///game/memory:50051";

/**
 * One memory entry as returned by `listMemories`: the service's internal
 * resource id (`memory_id`) and the entry text. The id is used only for
 * internal location (replace/remove old_text matching, snapshot rendering
 * excludes it) and MUST NOT be rendered into LLM-visible text
 * (specs/059-agent-v2-team-mode/spec.md FR-007).
 */
export interface MemoryEntry {
  memory_id: string;
  content: string;
}

/**
 * Build a Memory resource name per AIP-122:
 * `templates/{template}/sessions/{session}/memories/{memoryId}`.
 */
export function memoryName(
  template: string,
  session: string,
  memoryId: string,
): string {
  return `templates/${template}/sessions/${session}/memories/${memoryId}`;
}

/**
 * One decoded `ListMemoriesResponse.memories[]` wire entry (`longs: String`).
 */
interface ListedMemory {
  memoryId?: string;
  content?: string;
}

/**
 * Options for {@link MemoryStore.listMemories}. `orderBy` is forwarded to the
 * wire as the AIP-132 `order_by` value; `pageSize` switches the call to a
 * single-page read (see {@link MemoryStore.listMemories}).
 */
export interface ListMemoriesOptions {
  /** AIP-132 order_by value; an undefined value is not sent. */
  orderBy?: string;
  /** Page size for a single request; an undefined value is not sent. */
  pageSize?: number;
}

/**
 * The storage seam the memory operations consume: exactly the four
 * MemoryService RPCs this plugin needs (survey/deepseek-harness-memory-plugin.md
 * §7 option A — the plugin's whole storage dependency collapses onto this
 * interface, implemented by {@link MemoryClient}). Tests inject a
 * `vi.fn()`-backed double instead of a live gRPC client.
 */
export interface MemoryStore {
  createMemory(
    template: string,
    session: string,
    memoryId: string,
    content: string,
  ): Promise<void>;
  updateMemory(
    template: string,
    session: string,
    memoryId: string,
    content: string,
  ): Promise<void>;
  deleteMemory(template: string, session: string, memoryId: string): Promise<void>;
  listMemories(
    template: string,
    session: string,
    options?: ListMemoriesOptions,
  ): Promise<MemoryEntry[]>;
}

const TLS_CA_CERT = "/etc/tls/ca.crt";

/**
 * Build the TLS channel credentials for dominion gRPC services: TLS with the
 * deployment CA when present, insecure otherwise (local dev). Migrated
 * verbatim from the v1 agent's prompt-client.ts (the memory service is
 * reached through the same dominion endpoint policy).
 */
export function buildClientCredentials(): grpc.ChannelCredentials {
  if (!fs.existsSync(TLS_CA_CERT)) {
    return grpc.credentials.createInsecure();
  }

  const rootCert = fs.readFileSync(TLS_CA_CERT);
  return grpc.credentials.createSsl(rootCert);
}

// grpc-js defaults to pick_first, which pins a single connection; when that
// backend pod restarts the call hangs in "Waiting for LB pick" until reconnect.
// round_robin (matching grpc-go's ClientDefault) connects to every resolved
// endpoint, so a rolling-upgrade pod swap routes around the terminating pod.
export const ROUND_ROBIN_SERVICE_CONFIG = JSON.stringify({
  loadBalancingConfig: [{ round_robin: {} }],
});

// Deliberately NO app-level keepalive PINGs on this unary client — this
// mirrors grpc-go's `ClientDefault()` (common/gopkg/grpc/default.go:26): the
// Go side reserves HTTP/2 keepalive for long-lived streams, paired with
// server-side enforcement relaxation. The unary memory server runs grpc-go's
// DEFAULT enforcement policy (MinTime=5min, PermitWithoutStream=false), so a
// keepalive-enabled client's idle PINGs would be GOAWAY'd as "excess pings"
// and the connection torn down repeatedly.
//
// max_reconnect_backoff_ms: cap the subchannel retry interval (grpc-js
// default max backoff is 120s; 15s bounds how long a dead subchannel is
// retried before giving up the connection).
export const RECONNECT_OPTIONS: grpc.ChannelOptions = {
  "grpc.initial_reconnect_backoff_ms": 1_000,
  "grpc.max_reconnect_backoff_ms": 15_000,
};

/** Channel options used by the real construction path (and asserted by tests). */
export function buildChannelOptions(): grpc.ChannelOptions {
  const options: grpc.ChannelOptions = {
    ...RECONNECT_OPTIONS,
    "grpc.service_config": ROUND_ROBIN_SERVICE_CONFIG,
  };
  const serverName = process.env.TLS_SERVER_NAME;
  if (serverName && fs.existsSync(TLS_CA_CERT)) {
    options["grpc.ssl_target_name_override"] = serverName;
  }
  return options;
}

/**
 * The directory the deployment materializes `projects/game/game.proto` and
 * its `google/api` imports under (tools/release/deploy/README.md
 * §runtime_protos). From this package's compiled `src/` the root is four
 * levels up in both layouts:
 * `<repo>/common/js/dsh-plugins/memory-service/src` in the development tree
 * and `<service-root>/node_modules/@dominion/dsh-memory-service/src` inside
 * the service tar (workspace runtime packages preserve their source layout).
 */
function serviceRoot(): string {
  return path.resolve(import.meta.dirname, "..", "..", "..", "..");
}

/** The runtime game.proto path (exported for diagnostics/tests). */
export function resolveProtoPath(): string {
  return path.join(serviceRoot(), "projects", "game", "game.proto");
}

/** proto-loader options matching the ts_proto_library generation options. */
function protoOptions(): protoLoader.Options {
  return {
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
    includeDirs: [serviceRoot()],
  };
}

/**
 * Client for the MemoryService gRPC API.
 *
 * Registers the dominion resolver on construction, loads the game.proto
 * MemoryService definition, and creates a service-specific gRPC client that
 * resolves the memory service endpoint via the dominion URI scheme. The
 * optional `client` parameter allows dependency injection of a mock client
 * for testing without a live gRPC connection (DI seam — no `vi.mock`).
 */
export class MemoryClient implements MemoryStore {
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
      const memorySvc = (proto as any).projects.game.MemoryService;
      this.client = new memorySvc(
        MEMORY_SERVICE_TARGET,
        buildClientCredentials(),
        buildChannelOptions(),
      );
    }
  }

  /**
   * Create a memory entry under a session (AIP-133). The request carries the
   * resource EMBEDDED — `memory_id` (the service-internal storage key,
   * generated by the agent on add) plus the `Memory` body `{name, content}`.
   *
   * @throws {grpc.ServiceError} Propagates gRPC errors (ALREADY_EXISTS when
   *   the memory_id collides — callers dedupe first).
   */
  async createMemory(
    template: string,
    session: string,
    memoryId: string,
    content: string,
  ): Promise<void> {
    const name = memoryName(template, session, memoryId);
    await this.call<void>((client, metadata, options, cb) =>
      (client as any).createMemory(
        {
          parent: `templates/${template}/sessions/${session}`,
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
    session: string,
    memoryId: string,
    content: string,
  ): Promise<void> {
    await this.call<void>((client, metadata, options, cb) =>
      (client as any).updateMemory(
        {
          memory: {
            name: memoryName(template, session, memoryId),
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
    session: string,
    memoryId: string,
  ): Promise<void> {
    await this.call<void>((client, metadata, options, cb) =>
      (client as any).deleteMemory(
        { name: memoryName(template, session, memoryId) },
        metadata,
        options,
        cb,
      ),
    );
  }

  /**
   * List memory entries under a session.
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
    session: string,
    options?: ListMemoriesOptions,
  ): Promise<MemoryEntry[]> {
    const parent = `templates/${template}/sessions/${session}`;
    const singlePage = options?.pageSize !== undefined;
    const entries: MemoryEntry[] = [];
    let pageToken = "";
    do {
      const request: {
        parent: string;
        pageToken?: string;
        pageSize?: number;
        orderBy?: string;
      } = {
        parent,
        pageToken: pageToken || undefined,
      };
      if (options?.pageSize !== undefined) {
        request.pageSize = options.pageSize;
      }
      if (options?.orderBy !== undefined) {
        request.orderBy = options.orderBy;
      }
      const response = await this.call<{
        memories: ListedMemory[];
        nextPageToken?: string;
      }>((client, metadata, callOptions, cb) =>
        (client as any).listMemories(request, metadata, callOptions, cb),
      );
      for (const m of response?.memories ?? []) {
        if (m.memoryId != null && m.content != null) {
          entries.push({ memory_id: m.memoryId, content: m.content });
        }
      }
      pageToken = singlePage ? "" : (response?.nextPageToken ?? "");
    } while (pageToken.length > 0);
    return entries;
  }

  /** Close the underlying gRPC client connection. */
  close(): void {
    this.client.close();
  }

  /**
   * Shared callback-style RPC invocation (request + Metadata({waitForReady})
   * + deadline). A channel that dropped is probed first so the call does not
   * queue forever in "Waiting for LB pick".
   */
  private call<T>(
    invoke: (
      client: grpc.Client,
      metadata: grpc.Metadata,
      options: { deadline: Date },
      callback: (err: grpc.ServiceError | null, response?: T) => void,
    ) => void,
  ): Promise<T> {
    return new Promise<T>((resolve, reject) => {
      const deadline = new Date();
      deadline.setSeconds(deadline.getSeconds() + 10);
      probeChannel(this.client, "memory client call");
      invoke(
        this.client,
        new grpc.Metadata({ waitForReady: true }),
        { deadline },
        (err: grpc.ServiceError | null, response?: T) => {
          if (err) {
            error("memory client call failed", {
              error: err.message,
              code: String(err.code),
              channelState: probeChannel(
                this.client,
                "memory client call (after failure)",
              ),
            });
            reject(err);
            return;
          }
          resolve(response as T);
        },
      );
    });
  }
}

/**
 * Probe a grpc-js channel's connectivity state and nudge it to connect
 * (`getConnectivityState(true)` forces IDLE→CONNECTING): a call on an idle
 * channel then actively tries to reconnect instead of silently queueing until
 * the deadline. Logs the state for diagnosis and returns the state name.
 *
 * @param client The gRPC client whose channel to probe.
 * @param label  Log label identifying the call site.
 * @returns The connectivity state name, or a descriptive string when the
 *   channel is unavailable (injected test client without a channel).
 */
export function probeChannel(client: grpc.Client, label: string): string {
  let state = "no-channel";
  try {
    const channel = (
      client as unknown as { getChannel?: () => grpc.Channel }
    ).getChannel?.();
    if (channel) {
      const value = channel.getConnectivityState(true);
      state = grpc.connectivityState[value] ?? String(value);
    }
  } catch (err) {
    state = `error:${err instanceof Error ? err.message : String(err)}`;
  }
  info(`${label}: channel state`, { state });
  return state;
}
