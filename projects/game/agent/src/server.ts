/**
 * server.ts — Game agent gRPC server (TeamService).
 *
 * Loads game.proto, wires the team-service dependencies and registers the
 * TeamService (which replaced AgentService, specs/031-team-template-mode/
 * contracts/api-contract.md §2.2):
 *
 * - **PromptClient** → `getTeamProfile` (the saolei TeamProfile's
 *   player/planner model specs, §2.3).
 * - **ModelProviderCache** → per-model `ChatModel` singletons for the
 *   player/planner agents.
 * - **MemoryClient** → the planner's long-term memory data plane (039 US2,
 *   T022): a service-scoped gRPC client to the MemoryService
 *   (memory-mcp-contract.md §3). Per session the factory additionally builds
 *   the planner's memory MCP tools (via the mcp-host memory path —
 *   `buildMemoryMcpTools`, single hermes-style `memory` tool) and a
 *   `FrozenMemorySnapshot` (first bake at team init; refreshed at the
 *   compress boundary — team-graph-contract.md §3). Both are injected into
 *   `buildTeamGraph` — for the FIRST build AND the profile-change rebuild
 *   closure (the rebuilt planner holds the same memory tools + snapshot).
 *   The shared strategy/mongo wiring (Phase 5) is GONE (039 US3, T030/T031 —
 *   FR-013: no shared strategy store anywhere; the planner's calibration
 *   instructions are delivered via the `instruct_player` tool, T027).
 * - **SessionTeamStore** → per-session compiled saolei team graph (buildTeamGraph)
 *   with the saolei MCP tools wired as the player's tools (FR-010).
 * - **MCP host** → per-session saolei McpServer with the team sink injected
 *   (specs/031-team-template-mode/contracts/saolei-sink-contract.md §6;
 *   T009 extension point) + per-session memory McpServer (planner's `memory`
 *   tool; the host resolves the memory kind via the early-registration
 *   registry's MemoryClient — FR-007, memory-mcp-contract.md §4).
 */

import * as fs from "node:fs";
import * as path from "node:path";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import { info } from "@dominion/common-js-logs";
import { registerDominionResolver } from "@dominion/common-js-grpc-resolver";
import type { ProtoGrpcType } from "../game_types/game";

import { readSecret } from "./secrets";
import { PromptClient } from "./prompt-client";
import { MemoryClient } from "./memory-client";
import { ModelProviderCache } from "./model-provider";
import type { ChatModel } from "./model-provider";
import { SessionTeamStore } from "./session-team";
import { SessionTeam } from "./session-team";
import { Handler } from "./handler";
import { startMcpHost, DEFAULT_MCP_PORT } from "./mcp-host";
import { buildMemoryMcpTools, buildSaoleiMcpTools, defaultMcpClientFactory } from "./llm";
import { OperationBridge } from "./operation-bridge";
import { createEphemeralGameBuffer, createTeamSink } from "./team/team-sink";
import type { EphemeralGameBuffer } from "./team/team-sink";
import type { SaoleiEventSink } from "./mcp/saolei/saolei-mcp";
import { buildTeamGraph } from "./team/graph";
import { FrozenMemorySnapshot } from "./team/memory-snapshot";
import type { StructuredToolInterface } from "@langchain/core/tools";
import type { MemorySaver } from "@langchain/langgraph";

// ---------------------------------------------------------------------------
// Exported startServer
// ---------------------------------------------------------------------------

const protoRoot = path.join(__dirname, "..");
const protoPath = path.join(protoRoot, "projects", "game", "game.proto");
const protoIncludeDirs = [protoRoot];

function loadProto(): ProtoGrpcType {
  if (!fs.existsSync(protoPath)) {
    throw new Error(`game.proto not found at ${protoPath}`);
  }

  const packageDefinition = protoLoader.loadSync(protoPath, {
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
    includeDirs: protoIncludeDirs,
  });

  return grpc.loadPackageDefinition(
    packageDefinition,
  ) as unknown as ProtoGrpcType;
}

// ---------------------------------------------------------------------------
// TLS credentials (auto-detect)
// ---------------------------------------------------------------------------

function buildCredentials(): grpc.ServerCredentials {
  const tlsCert = "/etc/tls/tls.crt";
  const tlsKey = "/etc/tls/tls.key";
  const useTLS = fs.existsSync(tlsCert) && fs.existsSync(tlsKey);

  if (useTLS) {
    return grpc.ServerCredentials.createSsl(
      null,
      [{ cert_chain: fs.readFileSync(tlsCert), private_key: fs.readFileSync(tlsKey) }],
      false,
    );
  }
  return grpc.ServerCredentials.createInsecure();
}

// ---------------------------------------------------------------------------
// Exported startServer
// ---------------------------------------------------------------------------

export interface StartServerOverrides {
  /**
   * Model lookup override (DI seam — the test artifact
   * `bootstrap-test.ts` swaps the provider cache for the resolver-aware
   * fake-llm ChatModel; `style/javascript.md` §测试).
   */
  getProvider?: (modelSpec: string) => Promise<ChatModel>;
}

export async function startServer(
  overrides: StartServerOverrides = {},
): Promise<grpc.Server> {
  registerDominionResolver();

  const providerSecret = readSecret(
    path.join(process.env.DOMINION_SECRET_DIR || "/etc/secrets", "provider"),
  );

  const promptClient = new PromptClient();

  // 039 US2 (T022): the planner's long-term memory data plane — a single
  // service-scoped MemoryClient (memory-mcp-contract.md §3). The mcp-host's
  // memory mcp server forwards to the memory service via this client (the
  // mcp server never connects directly — FR-007).
  const memoryClient = new MemoryClient();

  const openaiBaseUrl =
    process.env.OPENCODE_OPENAI_BASE_URL ||
    process.env.OPENCODE_BASE_URL ||
    "https://opencode.ai/zen/go/v1";
  const anthropicBaseUrl =
    process.env.OPENCODE_ANTHROPIC_BASE_URL ||
    "https://opencode.ai/zen/go";
  const providerCache = new ModelProviderCache(
    openaiBaseUrl,
    anthropicBaseUrl,
    providerSecret,
  );
  const getProvider = overrides.getProvider ?? ((spec: string) => providerCache.getProvider(spec));

  // 039 US3 (T030/T031 — FR-013): the shared strategy/mongo wiring (Phase
  // 5) is REMOVED — the agent no longer persists any shared strategy. The
  // planner's long-term memory lives in the memory service (memoryClient
  // above), and its calibration instructions are delivered via the
  // `instruct_player` tool (built internally by the planner-family nodes,
  // T027 — staged through the configurable instructionBuffer installed per
  // turn by session-team.ts, contract §4 R1).

  // Per-session bridge/sink early-registration registry — the MCP host's
  // `SessionBridgeLookup` source. Entries are set by the team factory BEFORE
  // `buildSaoleiMcpTools`/`buildMemoryMcpTools` connect (and deleted when the
  // factory fails), so the host can build the session's McpServers during
  // team creation without the SessionTeam existing yet (circular-dependency
  // break — see the factory below). Shape matches `SessionBridgeLookup`'s
  // result (mcp-host.ts) with the sink always present. The `buffer` is kept
  // here too — the profile-change rebuild (US3) reuses the session's
  // ephemeral buffer (profile-independent, team-rebuild-contract.md §3). The
  // `memoryClient` (039 T022 — the memory-kind lookup source, FR-007) is
  // service-scoped and shared by all sessions.
  const sessionBridges = new Map<
    string,
    {
      bridge: OperationBridge;
      sink: SaoleiEventSink;
      buffer: EphemeralGameBuffer;
      memoryClient: MemoryClient;
    }
  >();

  // Per-session player tools (the saolei MCP-client tools, FR-010/FR-028).
  // Profile-independent and bound to the session's MCP-host server (cached
  // per sessionId — mcp-host.ts), so a profile-change rebuild reuses the SAME
  // tools instead of reconnecting (team-rebuild-contract.md §3/§4).
  const sessionPlayerTools = new Map<string, StructuredToolInterface[]>();

  // Per-session planner memory tools (the memory MCP-client tools, 039 T022 —
  // a single hermes-style `memory` tool, FR-007/FR-008). Same reuse rationale
  // as `sessionPlayerTools`: a profile-change rebuild reuses the SAME tools.
  const sessionMemoryTools = new Map<string, StructuredToolInterface[]>();

  // Per-session frozen memory snapshots (039 T022 — team-graph-contract.md
  // §3): the snapshot is per-session STATE (baked entries, frozen across
  // reviews, refreshed at the compress boundary), so a profile-change rebuild
  // MUST reuse the SAME instance — the rebuilt planner holds the identical
  // snapshot (T022 requirement).
  const sessionFrozenSnapshots = new Map<string, FrozenMemorySnapshot>();

  // Per-session team: resolve the requested TeamProfile's models (the
  // template + profile name come from the UpdateTeam request — no fixed
  // default profile), wire the saolei MCP tools as the player's tools
  // (FR-010/FR-028), compile the team graph.
  //
  // Circular-dependency break (large-test T030 finding): the MCP host's
  // `SessionBridgeLookup` must HIT while the player tools are being built,
  // because `buildSaoleiMcpTools` connects to the host's
  // `/internal/mcp/{sessionId}` endpoint inside the factory — before the
  // SessionTeamStore has cached the team. The host needs the session's
  // `{bridge, sink}` to build the McpServer, but the SessionTeam only exists
  // at the END of the factory. Both the bridge and the sink depend only on
  // the ephemeral buffer (which the factory creates early), so they are
  // pre-built and registered in {@link sessionBridges} BEFORE
  // `buildSaoleiMcpTools` connects, and the SAME instances are injected into
  // the SessionTeam — the graph player's operation bridge and the
  // mcp-host-served one are identical (specs/031-team-template-mode/
  // contracts/saolei-sink-contract.md §6).
  const sessionTeamStore = new SessionTeamStore(
    async (sessionId, template, profileName) => {
      const profile = await promptClient.getTeamProfile(
        template,
        profileName,
      );
      const playerModel = await getProvider(profile.playerModel);
      const plannerModel = await getProvider(profile.plannerModel);
      // FR-034: base prompts from the profile (empty string = unset = the
      // template default base, resolved inside the player/planner nodes).
      const playerBasePrompt = profile.playerPrompt ?? "";
      const plannerBasePrompt = profile.plannerPrompt ?? "";
      const buffer = createEphemeralGameBuffer();
      // sessionId/templateId are stamped on dispatched operation TeamFrames
      // (FR-013 envelope completeness — operation-bridge.ts dispatch).
      const bridge = new OperationBridge(sessionId, template);
      const sink = createTeamSink(buffer);
      sessionBridges.set(sessionId, { bridge, sink, buffer, memoryClient });
      try {
        const playerTools = await buildSaoleiMcpTools(
          template,
          sessionId,
          DEFAULT_MCP_PORT,
          defaultMcpClientFactory,
        );
        sessionPlayerTools.set(sessionId, playerTools);
        // 039 US2 (T022): the planner's memory tools come from the memory
        // mcp path — same MultiServerMCPClient pattern as the saolei tools
        // (memory-mcp-contract.md §4). The host builds the memory McpServer
        // lazily on this connect; the early-registered `memoryClient` (above)
        // makes the memory-kind lookup hit (FR-007 — the mcp server forwards
        // via the agent, never connects to the memory service directly).
        const memoryTools = await buildMemoryMcpTools(
          template,
          sessionId,
          DEFAULT_MCP_PORT,
          defaultMcpClientFactory,
        );
        sessionMemoryTools.set(sessionId, memoryTools);
        // First bake of the per-session frozen snapshot (team-init boundary,
        // team-graph-contract.md §3). The refresh degrades gracefully on
        // memory-service unavailability (keeps an empty snapshot — contract
        // §5: memory must not block team creation).
        const frozenSnapshot = new FrozenMemorySnapshot();
        await frozenSnapshot.refresh(memoryClient, template, sessionId);
        sessionFrozenSnapshots.set(sessionId, frozenSnapshot);
        const handle = buildTeamGraph({
          playerModel,
          plannerModel,
          // 044 US2 (T004): the profile's bare model specs feed the
          // per-reasoning-model idle-timeout floor (specs/044-llm-stall-
          // recovery-fix/contracts/idle-timeout-contract.md §3). The
          // playerModel/plannerModel instances above stay the sole model
          // wiring; these strings only resolve the node timeout.
          playerModelSpec: profile.playerModel,
          plannerModelSpec: profile.plannerModel,
          memoryClient,
          frozenSnapshot,
          template,
          buffer,
          sessionId,
          playerTools,
          plannerTools: memoryTools,
          playerBasePrompt,
          plannerBasePrompt,
        });
        return new SessionTeam(
          handle,
          buffer,
          sessionId,
          template,
          bridge,
          sink,
        );
      } catch (err) {
        // Team creation failed → drop the early registration so the session
        // is NOT visible to the MCP host (an orphaned entry would answer
        // /internal/mcp/... with a server bound to a team that never
        // materialized). The MCP host caches a created server per session, so
        // a retry after a post-connect failure reuses that server — the
        // pre-registered bridge/sink of a LATER retry then does not match it;
        // this edge (a failed team graph compile) is accepted and noted here.
        sessionBridges.delete(sessionId);
        sessionPlayerTools.delete(sessionId);
        sessionMemoryTools.delete(sessionId);
        sessionFrozenSnapshots.delete(sessionId);
        throw err;
      }
    },
    // Profile-change rebuild (US3, team-rebuild-contract.md §5): rebuilds
    // ONLY the team graph against the EXISTING checkpointer — the session's
    // conversation/game state is preserved (FR-005); buffer/bridge/sink/
    // MCP-host/tools are profile-independent and reused untouched (§3/§4).
    // The store calls this with the existing `handle.checkpointer` (never a
    // new MemorySaver — that would drop the history) and replaces the
    // SessionTeam's graphHandle on success; on failure the existing team is
    // left unchanged.
    async (
      sessionId: string,
      template: string,
      profileName: string,
      existingCheckpointer: MemorySaver,
    ) => {
      const profile = await promptClient.getTeamProfile(template, profileName);
      const playerModel = await getProvider(profile.playerModel);
      const plannerModel = await getProvider(profile.plannerModel);
      // FR-034: base prompts from the profile (empty string = unset = the
      // template default base, resolved inside the player/planner nodes).
      const playerBasePrompt = profile.playerPrompt ?? "";
      const plannerBasePrompt = profile.plannerPrompt ?? "";
      // Reuse the session's profile-independent instances (§3): the ephemeral
      // buffer (deps.buffer) and the already-connected saolei MCP tools
      // (bound to the host's cached per-session server).
      const buffer = sessionBridges.get(sessionId)?.buffer;
      const playerTools = sessionPlayerTools.get(sessionId);
      // 039 US2 (T022): the rebuilt graph's planner MUST hold the SAME memory
      // tools (bound to the host's cached per-session memory server) and the
      // SAME frozen snapshot (per-session state — a fresh snapshot would lose
      // the baked entries). memoryClient is service-scoped and shared.
      const memoryTools = sessionMemoryTools.get(sessionId);
      const frozenSnapshot = sessionFrozenSnapshots.get(sessionId);
      if (!buffer || !playerTools || !memoryTools || !frozenSnapshot) {
        // Unreachable for a materialized team (first build always registers
        // all of them) — defensive, so a rebuild can never silently drop state.
        throw new Error(
          `session ${sessionId}: rebuild preregistration missing (buffer/tools/snapshot)`,
        );
      }
      return buildTeamGraph(
        {
          playerModel,
          plannerModel,
          // 044 US2 (T004): same floor wiring as the first build — the
          // profile's bare model specs (specs/044-llm-stall-recovery-fix/
          // contracts/idle-timeout-contract.md §3).
          playerModelSpec: profile.playerModel,
          plannerModelSpec: profile.plannerModel,
          memoryClient,
          frozenSnapshot,
          template,
          buffer,
          sessionId,
          playerTools,
          plannerTools: memoryTools,
          playerBasePrompt,
          plannerBasePrompt,
        },
        existingCheckpointer,
      );
    },
  );

  const handler = new Handler(sessionTeamStore);

  // FR-001 + saolei-sink-contract.md §6: the localhost MCP HTTP host
  // resolves each (template, session, kind) triple to its per-kind
  // dependencies (R3 template-scoped multi-path scheme —
  // `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md`
  // §4). Both kinds are registered here: the saolei kind (the player's mcp —
  // bridge/sink) and the memory kind (the planner's mcp — 039 T022 wires the
  // memory-kind lookup that Phase 4 left undefined: the MemoryClient plus the
  // template/session path closure, FR-007/FR-012). The lookup reads the
  // early-registration registry (NOT the team store — the store only caches
  // the team AFTER the factory resolves, so it misses during
  // `buildSaoleiMcpTools`/`buildMemoryMcpTools`'s in-factory connect; the
  // registry hits). The template path segment is not re-validated here: a
  // session is bound to one template, and the registry is keyed by session id.
  startMcpHost(
    (template: string, sessionId: string, kind: "saolei" | "memory") => {
      const entry = sessionBridges.get(sessionId);
      if (!entry) {
        return undefined;
      }
      if (kind === "saolei") {
        return {
          kind: "saolei" as const,
          bridge: entry.bridge,
          sink: entry.sink,
        };
      }
      if (kind === "memory") {
        // The memory mcp server is built lazily by the host (on the planner's
        // first connect) with the injected MemoryClient — the mcp server
        // forwards to the memory service via the agent and NEVER connects to
        // it directly (FR-007).
        return {
          kind: "memory" as const,
          memoryClient: entry.memoryClient,
          template,
          session: sessionId,
        };
      }
      return undefined;
    },
  );

  const proto = loadProto();
  const credentials = buildCredentials();
  const tlsEnabled = fs.existsSync("/etc/tls/tls.crt") && fs.existsSync("/etc/tls/tls.key");

  const server = new grpc.Server({
    "grpc.max_receive_message_length": 8 * 1024 * 1024,
    "grpc.max_send_message_length": 8 * 1024 * 1024,
  });
  server.addService(
    proto.projects.game.TeamService.service,
    handler as any,
  );

  return new Promise((resolve, reject) => {
    server.bindAsync(
      "0.0.0.0:50051",
      credentials,
      (err, port) => {
        if (err) {
          reject(err);
          return;
        }
        server.start();
        info("gRPC server listening on 0.0.0.0:50051", { port, tls: tlsEnabled });
        resolve(server);
      },
    );
  });
}
