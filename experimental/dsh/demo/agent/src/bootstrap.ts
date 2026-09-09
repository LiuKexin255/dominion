/**
 * Agent lifecycle wiring for the bootstrap orchestrator.
 *
 * Exported for the entry point (main.ts) and for lifecycle unit tests —
 * this module performs no top-level side effects, so importing it never
 * boots the composition. The entry sequence (init → installReporter →
 * runAgent → uninstall/shutdown → exit) is fixed by
 * specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §8.
 *
 * Two-phase entry per
 * specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md §2:
 * the static import graph carries only OTel/bootstrap/log wiring; the dsh
 * composition boots inside a component and @grpc/grpc-js loads through the
 * dynamic import in the server component's start, after init() has
 * registered the OTel ESM loader hook.
 *
 * The orchestrator (@dominion/common-js-bootstrap) owns the k8s health
 * probe endpoint (:38080/healthz, started after every component and stopped
 * before any of them — specs/052-deploy-health-probe/contracts/
 * bootstrap-health.md §1) and the fail-loud startup semantics: any component
 * start failure rolls back the started peers and surfaces as a run()
 * rejection, so a half-started agent never serves traffic
 * (specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §1).
 */

import {
  Bootstrap,
  Stage,
  createGrpcServerComponent,
} from "@dominion/common-js-bootstrap";
import type {
  BootstrapOptions,
  Component,
  RunOptions,
} from "@dominion/common-js-bootstrap";
import { info } from "@dominion/common-js-logs";
import { bootDsh } from "./dsh.js";
import type { DshContext } from "./dsh.js";
import type { AgentSessions } from "./session.js";
import type { BuiltChatServer } from "./server.js";

/** Binds on all interfaces, matching the deployed container's grpc port. */
const CHAT_ADDRESS = "0.0.0.0:50051";

/**
 * The dsh boot and the chat-server construction are plain async functions,
 * so the lifecycle components are testable through injected doubles instead
 * of module interception (style/javascript.md Mock convention). Bootstrap
 * options and run options ride the same seam for tests only: production
 * MUST NOT pass bootstrapOptions (its healthServerFactory is the orchestrator's
 * @internal test seam), and both fields default to the production wiring.
 */
export interface AgentRunDeps {
  boot?: typeof bootDsh;
  buildChatServer?: (options: { ctx: DshContext }) => Promise<BuiltChatServer>;
  bootstrapOptions?: BootstrapOptions;
  runOptions?: RunOptions;
}

/**
 * Cross-component wiring: the composition component produces the dsh
 * Context the server component consumes, and the server component produces
 * the session registry the composition component disposes on the way down
 * (after the grpc server has drained — the orchestrator stops components in
 * strict reverse start order).
 */
interface AgentState {
  ctx?: DshContext;
  sessions?: AgentSessions;
  stopGrpc?: (signal: AbortSignal) => Promise<void>;
}

/**
 * The composition and chat-server lifecycle as two orchestrator components.
 *
 * Startup: composition (Client stage) boots the dsh plugin tree fail-loud —
 * any boot failure rejects the start and the orchestrator aborts the run
 * with no health endpoint listening. The chat server (Server stage) then
 * builds the (unbound) server and delegates bind/start to the orchestrator's
 * grpc adapter, whose stop implements tryShutdown racing the shutdown budget
 * followed by forceShutdown — the same graceful-drain semantics the hand
 * written bootstrap used to own (specs/047-dsh-chat-demo/contracts/
 * dsh-agent-service.md §1, specs/053-js-bootstrap-migration/contracts/
 * bootstrap-js-api.md §6).
 *
 * Shutdown (reverse order): grpc-chat drains in-flight calls within the
 * budget, then the composition component disposes every agent session and
 * the composition's root fiber
 * (specs/047-dsh-chat-demo/contracts/dsh-agent-service.md §1 order).
 */
function buildComponents(deps: AgentRunDeps, state: AgentState): Component[] {
  const boot = deps.boot ?? bootDsh;
  const buildChatServer =
    deps.buildChatServer ??
    (async (options: { ctx: DshContext }) => {
      // Dynamic import keeps @grpc/grpc-js out of the static import graph.
      const module = await import("./server.js");
      return module.buildServer(options);
    });

  const composition: Component = {
    name: "dsh-composition",
    stage: Stage.Client,
    async start(): Promise<void> {
      state.ctx = await boot();
    },
    async stop(): Promise<void> {
      await state.sessions?.shutdown();
    },
  };

  const chat: Component = {
    name: "grpc-chat",
    stage: Stage.Server,
    async start(signal): Promise<void> {
      const built = await buildChatServer({ ctx: state.ctx as DshContext });
      state.sessions = built.sessions;
      const grpc = createGrpcServerComponent("grpc-chat-server", {
        server: built.server,
        address: CHAT_ADDRESS,
        credentials: built.credentials,
      });
      await grpc.start(signal);
      state.stopGrpc = grpc.stop;
    },
    async stop(signal): Promise<void> {
      await state.stopGrpc?.(signal);
    },
  };

  return [composition, chat];
}

/**
 * Wire the agent lifecycle into a Bootstrap orchestrator and run it until a
 * clean signal exit. Resolves only on SIGTERM/SIGINT (or an injected run
 * signal); a component start failure — dsh composition boot included —
 * rejects after rolling back the started peers.
 */
export async function runAgent(deps: AgentRunDeps = {}): Promise<void> {
  const state: AgentState = {};
  const bootstrap = new Bootstrap({
    // The graceful-stop budget mirrors the hand written bootstrap's
    // tryShutdown grace period.
    shutdownTimeoutMs: 10_000,
    ...deps.bootstrapOptions,
  });
  for (const component of buildComponents(deps, state)) {
    bootstrap.register(component);
  }
  info("dsh demo agent starting", { service: "dsh-demo-agent", address: CHAT_ADDRESS });
  await bootstrap.run(deps.runOptions);
}
