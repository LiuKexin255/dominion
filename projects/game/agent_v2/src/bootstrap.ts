/**
 * Bootstrap entry point for the game agent_v2 service.
 *
 * Two-phase entry per
 * specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md §2:
 * the static import graph carries only OTel/bootstrap wiring; @grpc/grpc-js
 * and mongodb load through the dynamic imports below, after init() has
 * registered the OTel ESM loader hook. The entry sequence (init →
 * installReporter → dynamic import → register → run → uninstall/shutdown →
 * exit) is fixed by
 * specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §8; the
 * migration sample is experimental/js/grpc_hello_world/src/bootstrap.ts.
 *
 * Component lifecycle (specs/051-agent-v2-dsh-migration/contracts/
 * saolei-plugins.md §6 invariant): the preset Mongo storage starts first and
 * stops last; the dsh composition (agent sessions + root fiber) starts
 * second and stops between the gRPC server and Mongo — the server drains
 * first, then every session disposes (in-flight turns aborted) followed by
 * the composition's root fiber, then the Mongo client closes. Stages
 * (Foundation 100 < Client 200 < Server 300) encode that order: start runs
 * stage-ascending, stop runs strictly reversed. The 38080/healthz endpoint
 * is built into Bootstrap and serves only after every component has started
 * (specs/052-deploy-health-probe/contracts/deploy-probe.md).
 */

import {
  Bootstrap,
  createGrpcServerComponent,
  Stage,
  type Component,
} from "@dominion/common-js-bootstrap";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import {
  createOTelReporter,
  error,
  info,
  installReporter,
} from "@dominion/common-js-logs";
import { init, shutdown } from "@dominion/common-js-otel";
// Type-only: erased at compile time, so none of these put @grpc/grpc-js or
// mongodb into the static import graph ahead of the OTel loader hook.
import type { MongoClient } from "mongodb";
import type { AgentSessions } from "./session.js";
import type { PresetStore } from "./presets.js";
import type { DshContext } from "./dsh.js";

// Binds on all interfaces, matching the deployed container port declared in
// projects/game/agent_v2/service.yaml.
const GRPC_ADDRESS = "0.0.0.0:50051";

async function main(): Promise<void> {
  // 1. OTel init registers the ESM loader hook and the gRPC instrumentation,
  // so @grpc/grpc-js is patched when the dynamic imports below load it.
  await init({ instrumentations: [createGrpcInstrumentation()] });

  // 2. Structured logs export through the OTel reporter.
  const uninstallReporter = installReporter(createOTelReporter("game/agent-v2"));
  info("service starting", { service: "game-agent-v2" });

  // Safety net for the long-lived streaming surface: a write racing a peer
  // disconnect could escape an async listener as an unhandled rejection,
  // which would terminate the process and every live session with it. The
  // primary guards are the Send stream's 'error' listener and safeWrite in
  // server.ts; this handler covers any future regression of the same
  // category (v1 precedent: projects/game/agent/src/bootstrap.ts).
  process.on("unhandledRejection", (reason) => {
    error("unhandled promise rejection", { reason: String(reason) });
  });

  let exitCode = 0;
  try {
    // 3. Dynamic imports keep @grpc/grpc-js and mongodb out of the static
    // import graph (one uniform wiring point, the grpc_hello_world pattern).
    const { bootDsh } = await import("./dsh.js");
    const { MongoClient } = await import("mongodb");
    const {
      MongoPresetStore,
      PRESET_COLLECTION_NAME,
      PRESET_DATABASE,
      mongoPresetCollection,
      resolveMongoUri,
    } = await import("./presets.js");
    const { buildServer } = await import("./server.js");

    // Cells filled by component starts: the composition stop reads the
    // sessions created by the gRPC server start, and the gRPC server start
    // consumes the composition and preset store from the earlier stages
    // (start order is stage-ascending, stop strictly reversed).
    let mongo: MongoClient | undefined;
    let presetStore: PresetStore | undefined;
    let ctx: DshContext | undefined;
    let sessions: AgentSessions | undefined;
    let bound: Component | undefined;

    const presetStoreComponent: Component = {
      name: "preset-store",
      stage: Stage.Foundation,
      start: async () => {
        // Fail-loud preset storage
        // (specs/051-agent-v2-dsh-migration/spec.md FR-005): the service
        // serves preset CRUD from its own Mongo database, so a
        // connect/index failure must never half-start the surface — the
        // failure rejects the start and Bootstrap rolls back and exits
        // non-zero through the main catch.
        const client = new MongoClient(await resolveMongoUri());
        mongo = client;
        await client.connect();
        const store = new MongoPresetStore(
          mongoPresetCollection(client.db(PRESET_DATABASE).collection(PRESET_COLLECTION_NAME)),
        );
        await store.ensureIndexes();
        presetStore = store;
        info("preset store connected", { database: PRESET_DATABASE, collection: PRESET_COLLECTION_NAME });
      },
      stop: async () => {
        // Closed after the fiber: nothing may outlive the Mongo handle it
        // serves from.
        await mongo?.close();
        mongo = undefined;
        presetStore = undefined;
      },
    };

    const dshComponent: Component = {
      name: "dsh",
      stage: Stage.Client,
      start: async () => {
        // Fail-loud composition boot: resolves only on a fully settled
        // plugin tree.
        ctx = await bootDsh();
      },
      stop: async () => {
        // Disposes every session (in-flight turns aborted, queued messages
        // dropped) and then the composition's root fiber. `sessions` is
        // created by the gRPC server start, which stops before this.
        await sessions?.shutdown();
        sessions = undefined;
        ctx = undefined;
      },
    };

    const grpcComponent: Component = {
      name: "grpc",
      stage: Stage.Server,
      start: async (signal) => {
        // The server object needs the booted composition and the connected
        // preset store — both earlier-stage component starts.
        if (ctx === undefined || presetStore === undefined) {
          throw new Error("bootstrap: dsh composition / preset store not started");
        }
        const { server, credentials, sessions: live } = buildServer({
          ctx,
          presetStore,
        });
        sessions = live;
        // Binding and the graceful stop (tryShutdown racing the shutdown
        // budget → forceShutdown) are the adapter's semantics
        // (specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §6).
        bound = createGrpcServerComponent("grpc", { server, address: GRPC_ADDRESS, credentials });
        await bound.start(signal);
      },
      stop: async (signal) => {
        await bound?.stop(signal);
        bound = undefined;
      },
    };

    const bootstrap = new Bootstrap();
    bootstrap.register(presetStoreComponent);
    bootstrap.register(dshComponent);
    bootstrap.register(grpcComponent);

    // 4. Runs until SIGTERM/SIGINT; resolves only on a clean signal exit,
    // with the health endpoint stopped first (FIFO lifecycle).
    await bootstrap.run();
  } catch (err) {
    console.error("Fatal error:", err);
    exitCode = 1;
  }

  // 5. OTel lifecycle stays entry-level glue, not a component
  // (specs/053-js-bootstrap-migration/research.md D4): shutdown runs after
  // every component has stopped so shutdown-period logs still export.
  uninstallReporter();
  await shutdown();
  process.exit(exitCode);
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
