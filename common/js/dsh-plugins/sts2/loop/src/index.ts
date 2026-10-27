/**
 * Package entry for `@dominion/dsh-sts2-loop`
 * (`common/js/dsh-plugins/sts2/loop/`): the sts2 minimal orchestration's
 * public surface and its cordis composition row.
 *
 * Directory and package-name mapping:
 * specs/079-sts2-template-scaffold/contracts/dsh-plugins-layout.md §2.
 * Public surface and behaviour:
 * specs/080-host-main-sub-model/contracts/sts2-loop-minimal.md §2.
 *
 * The row is an empty lazy row (specs/080-host-main-sub-model/contracts/
 * sts2-loop-minimal.md §1): it provides no service and registers no tools or
 * prompt sections, so mounting it exercises the deploy/bare-specifier channel
 * without runtime behaviour. The orchestrator instance is constructed per
 * session by the host's scene registration, never as a composition singleton.
 */

import type { Context } from "@deepseek-ai/cordis";

export { Sts2Orchestrator, Sts2StateError } from "./orchestrator.js";
export type {
  AgentCreationSeam,
  ComposedPreset,
  ComposePreset,
  OrchestratorLogger,
  Sts2Failure,
  Sts2FailureDetail,
  Sts2MaterializeInput,
  Sts2Member,
  Sts2MemberConfig,
  Sts2OrchestrationFailureContext,
  Sts2OrchestratorDeps,
  Sts2Snapshot,
  Sts2StateErrorCode,
} from "./orchestrator.js";
export type { MainDriveErrorCode } from "./drive.js";

/** Composition row id (projects/game/agent_v2/cordis.yml). */
export const name = "sts2-loop";

/** Lazy row: the assembly needs no injected service at load time. */
export const inject: string[] = [];

/** Lazy row: no service, tool or prompt section is registered here. */
export function apply(_ctx: Context): void {}
