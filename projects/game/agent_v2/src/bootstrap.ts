/**
 * Bootstrap entry point for the game agent_v2 service.
 *
 * Order matters (demo pattern, specs/047-dsh-chat-demo/contracts/
 * dsh-agent-service.md §1; specs/049-agent-v2-dsh-init/spec.md FR-002):
 * OTel + gRPC instrumentation initializes BEFORE @grpc/grpc-js loads; the
 * dsh composition boots fail-loud (any failure exits non-zero); the preset
 * store's Mongo client connects fail-loud (preset CRUD is part of the
 * served surface, specs/051-agent-v2-dsh-migration/spec.md FR-005); only
 * then does the gRPC server module load and start serving. SIGTERM/SIGINT
 * triggers the graceful chain in dependency order (specs/051-agent-v2-dsh-migration/contracts/saolei-plugins.md §6):
 * stop the server → dispose every agent session (in-flight turns aborted)
 * → dispose the composition's root fiber (both inside sessions.shutdown)
 * → close the Mongo client → flush OTel → exit 0.
 */

import type { Server } from "@grpc/grpc-js";
import type { MongoClient } from "mongodb";
import { init, shutdown } from "@dominion/common-js-otel";
import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
import { error, info, installReporter, createOTelReporter } from "@dominion/common-js-logs";
import { bootDsh } from "./dsh.js";

/**
 * Graceful server stop: tryShutdown waits for in-flight RPCs to finish; the
 * bounded-time fallback to forceShutdown keeps a stuck call from blocking the
 * agent/fiber teardown behind it. `Server` is a type-only import so this
 * module still loads no @grpc/grpc-js runtime code before OTel init.
 */
function gracefulStop(server: Server, timeoutMs = 10_000): Promise<void> {
  return new Promise((resolve) => {
    const timer = setTimeout(() => {
      server.forceShutdown();
      resolve();
    }, timeoutMs);
    server.tryShutdown(() => {
      clearTimeout(timer);
      resolve();
    });
  });
}

async function main(): Promise<void> {
  await init({ instrumentations: [createGrpcInstrumentation()] });
  const uninstallReporter = installReporter(createOTelReporter("game/agent-v2"));
  info("otel initialized", { service: "game-agent-v2" });

  // Safety net for the long-lived streaming surface: a write racing a peer
  // disconnect could escape an async listener as an unhandled rejection,
  // which would terminate the process and every live session with it. The
  // primary guards are the Send stream's 'error' listener and safeWrite in
  // server.ts; this handler covers any future regression of the same
  // category (v1 precedent: projects/game/agent/src/bootstrap.ts).
  process.on("unhandledRejection", (reason) => {
    error("unhandled promise rejection", { reason: String(reason) });
  });

  // Fail-loud composition boot: resolves only on a fully settled plugin tree.
  const ctx = await bootDsh();

  // Dynamic imports defer @grpc/grpc-js loading until after OTel init (the
  // proto-loader stack inside server.js and the preset-store module both sit
  // behind it); mongodb is not an instrumented package but travels with the
  // same deferred block for one uniform wiring point.
  const { startServer } = await import("./server.js");
  const { MongoClient } = await import("mongodb");
  const {
    MongoPresetStore,
    PRESET_COLLECTION_NAME,
    PRESET_DATABASE,
    mongoPresetCollection,
    resolveMongoUri,
  } = await import("./presets.js");

  // Fail-loud preset storage (specs/051-agent-v2-dsh-migration/spec.md
  // FR-005): the service serves preset CRUD from
  // its own Mongo database, so a connect/index failure must never half-start
  // the surface — the process exits non-zero through the main catch.
  const mongoUri = await resolveMongoUri();
  const mongo: MongoClient = new MongoClient(mongoUri);
  await mongo.connect();
  const presetStore = new MongoPresetStore(
    mongoPresetCollection(mongo.db(PRESET_DATABASE).collection(PRESET_COLLECTION_NAME)),
  );
  await presetStore.ensureIndexes();
  info("preset store connected", { database: PRESET_DATABASE, collection: PRESET_COLLECTION_NAME });

  const started = await startServer({ ctx, presetStore });
  info("service started", { service: "game-agent-v2", port: 50051 });

  let exiting = false;
  const shutdownHandler = async (signal: string): Promise<void> => {
    if (exiting) return;
    exiting = true;
    info("shutting down", { signal, service: "game-agent-v2" });
    try {
      await gracefulStop(started.server);
      // Disposes every session (in-flight turns aborted, queued messages
      // dropped) and then the composition's root fiber.
      await started.sessions.shutdown();
      // The client is closed after the fiber: nothing may outlive the
      // Mongo handle it serves from.
      await mongo.close();
    } catch (err) {
      error("shutdown cleanup failed", {
        error: err instanceof Error ? err.message : String(err),
      });
    }
    uninstallReporter();
    await shutdown();
    process.exit(0);
  };

  process.on("SIGTERM", () => void shutdownHandler("SIGTERM"));
  process.on("SIGINT", () => void shutdownHandler("SIGINT"));
}

main().catch((err) => {
  console.error("Fatal error:", err);
  process.exit(1);
});
