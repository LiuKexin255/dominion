/**
 * cordis plugin entry for the saolei memory capability adapter (the thin
 * provider the saolei-loop composition mounts as a `config.plugins`
 * sub-plugin; specs/074-dsh-grpc-runtime/contracts/team-assembly.md §3.2).
 *
 * The adapter owns the scenario's deployment knowledge and injects the
 * **memory capability only**: it constructs the MemoryService client once at
 * plugin load — driven by its config (`target`, canonical `protoPath`,
 * optional `label`) and the shared `ctx.grpc` runtime — and hands the
 * composition-level unary policy (`ctx.grpc.callUnary`) to the domain-agnostic
 * library, so client construction and invocation policy both stay owned by the
 * runtime (specs/076-agent-v2-optimize/contracts/memory-library-layering.md
 * §1 义务 2/§3). The per-member `memoryMount` factory is exposed through
 * `ctx.provide`; `build(scope)` calls the library's memory-unit factory with
 * the scope the orchestration passes explicitly, and the adapter never derives
 * the scope and never registers anything (the orchestration registers the unit
 * into the member scope).
 *
 * The mount's product therefore carries no wiki face, no wiki tool and no wiki
 * prompt section: the materialized planner is free of wiki traces
 * (specs/076-agent-v2-optimize/spec.md FR-001/SC-002). The wiki capability
 * itself stays intact in the library's wiki unit for consumers that mount it.
 *
 * Dependency direction (contracts/team-assembly.md §6): adapter →
 * memory library (`@dominion/common-js-memory`) + the `grpc` service face.
 * The adapter MUST NOT depend on the saolei-loop orchestration — the scope
 * travels as an explicit `build(scope)` argument, so the capability can be
 * consumed by any orchestrator. The `grpc` face is read structurally
 * (`GrpcClientSeam`) so this package stays independent of the grpc-runtime
 * plugin package; the runtime inject declares the hard requirement.
 */

import type { Context } from "@deepseek-ai/cordis";
import z from "@deepseek-ai/schemastery";

import { buildMemoryUnit } from "@dominion/common-js-memory";
import type {
  CapabilityUnit,
  MemoryScope,
  MemoryServiceCalls,
  UnaryPolicy,
} from "@dominion/common-js-memory";

/** The plugin identifier the composition manifest references. */
export const name = "saolei-memory";

/** The composition-level gRPC client runtime this adapter consumes. */
export const inject = ["grpc"];

/** The provide name the saolei-loop orchestration reads the factory from. */
export const MEMORY_MOUNT_SERVICE = "memoryMount";

/** The fully-qualified MemoryService name of the game proto. */
export const MEMORY_SERVICE_NAME = "projects.game.MemoryService";

/** Config: the scenario's deployment connection inputs. */
export interface SaoleiMemoryConfig {
  /** dominion URI, e.g. `dominion:///game/memory:50051`. */
  readonly target: string;
  /**
   * Canonical proto import path (e.g. `projects/game/game.proto`) the
   * composition-level runtime resolves to a runtime absolute path; an
   * absolute path is also accepted as the escape hatch.
   */
  readonly protoPath: string;
  /** Optional log label; the runtime defaults it to the service name. */
  readonly label?: string;
}

// Schemastery's ObjectS input shape is wider than the validated output type;
// the cast mirrors the official adapters' declared `Config: z<Config>` shape
// (common/js/dsh-plugins/llm-glm/src/index.ts precedent).
export const Config: z<SaoleiMemoryConfig> = z.object({
  target: z.string().required(),
  protoPath: z.string().required(),
  label: z.string(),
}) as unknown as z<SaoleiMemoryConfig>;

/**
 * Structural view of the `ctx.grpc` client-construction face
 * (specs/074-dsh-grpc-runtime/contracts/grpc-runtime.md §2). Declared locally
 * so the adapter does not compile-depend on the grpc-runtime plugin package;
 * the runtime inject guarantees the real service is composed.
 */
export interface GrpcClientOptions {
  readonly target: string;
  readonly protoPath: string;
  readonly serviceName: string;
  readonly label?: string;
}

/** The `ctx.grpc` service face the adapter consumes. */
export interface GrpcClientSeam {
  client<T>(options: GrpcClientOptions): T;
  /** The shared unary policy passed through to the library's unit factory. */
  callUnary: UnaryPolicy;
}

/** The per-member capability factory the orchestration consumes. */
export interface MemoryMount {
  /** Build one member's memory unit with the scope passed explicitly. */
  build(scope: MemoryScope): CapabilityUnit;
}

/**
 * Apply the plugin: construct the memory service client once over the shared
 * `ctx.grpc` channel cache and provide the `memoryMount` factory. `build` is a
 * pure delegation into {@link buildMemoryUnit} — no I/O happens until the
 * orchestration awaits `unit.ready()`.
 */
export function apply(ctx: Context, config: SaoleiMemoryConfig): void {
  const grpc = ctx.get("grpc") as GrpcClientSeam | undefined;
  if (grpc === undefined) {
    throw new Error(
      "saolei-memory: the grpc service is not composed; the " +
        "composition-level gRPC runtime (@dominion/dsh-grpc-runtime) must " +
        "be a row and this adapter declares `inject: [\"grpc\"]`",
    );
  }

  const label = config.label === undefined ? {} : { label: config.label };
  const memoryClient = grpc.client<MemoryServiceCalls>({
    target: config.target,
    protoPath: config.protoPath,
    serviceName: MEMORY_SERVICE_NAME,
    ...label,
  });

  ctx.provide(MEMORY_MOUNT_SERVICE, {
    build(scope: MemoryScope): CapabilityUnit {
      return buildMemoryUnit({ scope, memoryClient, callUnary: grpc.callUnary });
    },
  } satisfies MemoryMount);
}
