/**
 * Composition-level gRPC client runtime (`ctx.grpc`).
 *
 * Builds typed service clients from a canonical proto identity, sharing one
 * channel per target across services and caching the loaded package
 * definition. The client construction is the single proto-object cast point
 * in the repository (specs/074-dsh-grpc-runtime/spec.md FR-012); all
 * connection policy lives in `./channel.js`, all unary policy in
 * `./unary.js`. Contract: specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md
 * §2.
 */

import * as path from "node:path";

import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";

import { ChannelPool } from "./channel.js";
import { callUnary } from "./unary.js";
import type { UnaryInvoke } from "./unary.js";

/** Options for one typed service-client construction (contracts §2). */
export interface GrpcClientOptions {
  /** Dominion URI, e.g. `"dominion:///game/memory:50051"`. */
  readonly target: string;
  /**
   * Canonical import path (e.g. `"projects/game/game.proto"`; resolved
   * against the module-relative service root per the runtime_protos deploy
   * standard, tools/release/deploy/README.md §runtime_protos) or an absolute
   * path (escape hatch).
   */
  readonly protoPath: string;
  /** Proto include dirs; defaults to `[resolved service root]`. */
  readonly includeDirs?: readonly string[];
  /** Fully-qualified service name, e.g. `"projects.game.MemoryService"`. */
  readonly serviceName: string;
  /**
   * Diagnostics label used in construction error messages; defaults to
   * {@link GrpcClientOptions.serviceName}.
   */
  readonly label?: string;
}

/** The service face mounted as `ctx.grpc` (contracts §2). */
export interface GrpcRuntime {
  client<T>(options: GrpcClientOptions): T;
  callUnary<T>(
    client: grpc.Client,
    label: string,
    invoke: UnaryInvoke<T>,
    deadlineMs?: number,
  ): Promise<T>;
}

/** A runtime instance plus its composition-lifecycle teardown. */
export interface GrpcRuntimeHandle {
  readonly runtime: GrpcRuntime;
  /** Close every cached channel (registered as a composition effect). */
  close(): void;
}

/** Injection seams for tests (DI; no module interception). */
export interface GrpcRuntimeDeps {
  readonly loadSync?: (
    filename: string | string[],
    options?: protoLoader.Options,
  ) => protoLoader.PackageDefinition;
  readonly loadPackageDefinition?: (
    packageDefinition: protoLoader.PackageDefinition,
  ) => grpc.GrpcObject;
  readonly pool?: ChannelPool;
}

/**
 * The directory the deployment materializes canonical proto paths under
 * (tools/release/deploy/README.md §runtime_protos). From this module's
 * compiled `src/` the service root is four levels up in the production
 * layout: `<service-root>/node_modules/@dominion/dsh-grpc-runtime/src`
 * (workspace runtime packages preserve their source layout). The
 * development-tree parent is not a service root — proto loading is a
 * deployment concern and tests inject the loader.
 */
const SERVICE_ROOT = path.resolve(import.meta.dirname, "..", "..", "..", "..");

/**
 * Resolve a proto identity to the filesystem path the loader reads:
 * absolute paths pass through (escape hatch), canonical import paths resolve
 * against {@link SERVICE_ROOT}.
 */
export function resolveProtoPath(protoPath: string): string {
  return path.isAbsolute(protoPath)
    ? protoPath
    : path.join(SERVICE_ROOT, protoPath);
}

/** The default include dir for a canonical proto path. */
function defaultIncludeDirs(): string[] {
  return [SERVICE_ROOT];
}

/**
 * Walk the loaded package definition by the fully-qualified service name and
 * return its client constructor. Missing segments fail loud — a typo in
 * `serviceName` must not silently build a proto-type object. `label` (the
 * options label defaulting to `serviceName`) identifies the failing request in
 * the message.
 */
function resolveServiceConstructor(
  proto: grpc.GrpcObject,
  serviceName: string,
  label: string,
): grpc.ServiceClientConstructor {
  let node:
    | grpc.GrpcObject
    | grpc.ServiceClientConstructor
    | grpc.ProtobufTypeDefinition = proto;
  for (const segment of serviceName.split(".")) {
    if (typeof node !== "object" || node === null || !(segment in node)) {
      throw new Error(
        `grpc-runtime: service "${label}" not found in the loaded proto ` +
          `(missing segment "${segment}")`,
      );
    }
    node = (node as grpc.GrpcObject)[segment];
  }
  if (typeof node !== "function") {
    throw new Error(
      `grpc-runtime: "${label}" resolves to a proto type, not a service ` +
        "client constructor",
    );
  }
  return node;
}

/**
 * Create a runtime instance: per-instance channel and package-definition
 * caches, the service-client builder, and the unary helper.
 */
export function createGrpcRuntime(
  deps: GrpcRuntimeDeps = {},
): GrpcRuntimeHandle {
  const loadSync = deps.loadSync ?? protoLoader.loadSync;
  const loadPackageDefinition =
    deps.loadPackageDefinition ?? grpc.loadPackageDefinition;
  const pool = deps.pool ?? new ChannelPool();
  const packageDefinitions = new Map<string, protoLoader.PackageDefinition>();

  function getPackageDefinition(
    protoPath: string,
    includeDirs: readonly string[],
  ): protoLoader.PackageDefinition {
    // Cache key = resolved protoPath + includeDirs (contracts §2 O2): the same
    // canonical identity with a different include set is a different parse.
    const key = `${protoPath}\u0000${includeDirs.join("\u0000")}`;
    const cached = packageDefinitions.get(key);
    if (cached !== undefined) {
      return cached;
    }
    // Loader options MUST stay in lockstep with the ts_proto_library
    // generation options (longs/enums/defaults/oneofs; keepCase omitted) —
    // tools/dev/js/ts_proto_library.bzl.
    const loaded = loadSync(protoPath, {
      longs: String,
      enums: String,
      defaults: true,
      oneofs: true,
      includeDirs: [...includeDirs],
    });
    packageDefinitions.set(key, loaded);
    return loaded;
  }

  function client<T>(options: GrpcClientOptions): T {
    const includeDirs = options.includeDirs
      ? [...options.includeDirs]
      : defaultIncludeDirs();
    const protoPath = resolveProtoPath(options.protoPath);
    const packageDefinition = getPackageDefinition(protoPath, includeDirs);
    const proto = loadPackageDefinition(packageDefinition);
    const label = options.label ?? options.serviceName;
    const ServiceClient = resolveServiceConstructor(
      proto,
      options.serviceName,
      label,
    );
    const channel = pool.get(options.target);
    // grpc-js's Client constructor takes `channelOverride` directly and never
    // consults the address/credentials in that branch
    // (https://github.com/grpc/grpc-node/issues/2214): the channel — and the
    // deployment credentials — is owned by the pool, so no second
    // buildClientCredentials() filesystem read happens per client.
    const instance = new ServiceClient(
      options.target,
      grpc.credentials.createInsecure(),
      { channelOverride: channel },
    );
    // The single proto-object cast point in the repository: the caller
    // supplies the structural call-surface type (contracts §2 O1, FR-012).
    return instance as T;
  }

  const runtime: GrpcRuntime = {
    client,
    callUnary: (client, label, invoke, deadlineMs) =>
      callUnary(client, label, invoke, deadlineMs),
  };

  return {
    runtime,
    close: () => pool.closeAll(),
  };
}
