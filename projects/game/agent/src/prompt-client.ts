/**
 * PromptClient — gRPC client for the PromptService.
 *
 * Resolves the prompt service via the dominion resolver and calls the
 * GetTeamProfile RPC to fetch the template-specialized team configuration
 * (specs/031-team-template-mode/contracts/api-contract.md §2.3). The saolei
 * template's TeamProfile carries the player/planner LLM model selection
 * (SaoleiProfile); the strategy is NOT managed by the prompt service
 * (specs/031-team-template-mode/contracts/strategy-store-contract.md).
 */
import * as fs from "node:fs";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import * as path from "node:path";
import {
  registerDominionResolver,
  createDeployClient,
} from "@dominion/common-js-grpc-resolver";

/**
 * Path to game.proto, relative to the compiled src/ directory.
 *
 * Exported so sibling gRPC clients (e.g. `memory-client.ts`, spec 039 T014 —
 * `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md` §3)
 * load the SAME proto definition with the SAME options instead of
 * duplicating the path/loader config.
 */
export const PROTO_PATH = path.join(
  __dirname,
  "..",
  "projects", "game", "game.proto",
);

/**
 * Proto loader options MUST match ts_proto_library generation options.
 * Exported for the same single-source-of-truth reason as `PROTO_PATH`.
 */
export const PROTO_OPTIONS: protoLoader.Options = {
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true,
  includeDirs: [path.join(__dirname, "..")],
};

/** Dominion resolver target for the prompt service. */
export const PROMPT_SERVICE_TARGET = "dominion:///game/prompt:50051";

const TLS_CA_CERT = "/etc/tls/ca.crt";

/** Return type for PromptClient.getTeamProfile(). */
export interface TeamProfileResult {
  /** The player agent's LLM model spec (SaoleiProfile.player_model). */
  playerModel: string;
  /** The planner agent's LLM model spec (SaoleiProfile.planner_model). */
  plannerModel: string;
  /**
   * The player's base prompt (SaoleiProfile.player_prompt; empty string =
   * unset = template default base, FR-034).
   */
  playerPrompt: string;
  /**
   * The planner's base prompt (SaoleiProfile.planner_prompt; empty string =
   * unset = template default base, FR-034).
   */
  plannerPrompt: string;
}

/**
 * Build the TLS channel credentials for dominion gRPC services: TLS with the
 * deployment CA when present, insecure otherwise (local dev).
 *
 * Exported so sibling gRPC clients (`memory-client.ts`, spec 039 T014) reuse
 * the same TLS policy — see `PROTO_PATH` above.
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

// HTTP/2 PING-based keepalive to detect silently-dropped idle connections.
// Node.js (unlike Go's stdlib) does NOT enable TCP-level SO_KEEPALIVE by
// default (Go sets it with a 15s interval — grpc-go issue #6250), so without
// these options a half-open TCP connection is only detected by the OS TCP
// retransmission timeout (~15 min on Linux), leaving the channel stuck.
//
// permit_without_calls=1: send PINGs even when idle. The grpc-go server's
// default EnforcementPolicy (MinTime=5min, PermitWithoutStream=false) will
// GOAWAY the connection after repeated idle PINGs, but per gRPC A8 the
// client auto-doubles keepalive_time until it exceeds MinTime — the interval
// self-stabilizes at ~5min without any server-side changes.
// https://github.com/grpc/proposal/blob/master/A8-client-side-keepalive.md
//
// max_reconnect_backoff_ms: cap subchannel retry interval (grpc-js default is
// 120s; 15s ensures faster recovery once a dead connection is detected).
export const KEEPALIVE_OPTIONS: grpc.ChannelOptions = {
  "grpc.keepalive_time_ms": 30_000,
  "grpc.keepalive_timeout_ms": 10_000,
  "grpc.keepalive_permit_without_calls": 1,
  "grpc.initial_reconnect_backoff_ms": 1_000,
  "grpc.max_reconnect_backoff_ms": 15_000,
};

export function buildChannelOptions(): grpc.ChannelOptions {
  const options: grpc.ChannelOptions = {
    ...KEEPALIVE_OPTIONS,
    "grpc.service_config": ROUND_ROBIN_SERVICE_CONFIG,
  };
  const serverName = process.env.TLS_SERVER_NAME;
  if (serverName && fs.existsSync(TLS_CA_CERT)) {
    options["grpc.ssl_target_name_override"] = serverName;
  }
  return options;
}

/**
 * Exposes the channel-options construction as a factory seam (FR-009). Tests
 * assert the load-balancing options directly without constructing a real gRPC
 * client — which previously required fragile module-level `vi.mock` of
 * `@grpc/grpc-js` / `@grpc/proto-loader` (bypassed by the pre-compiled `:lib`
 * under Bazel js_test — see research.md §2 and style/javascript.md §测试).
 */
export function buildChannelOptionsForTest(): grpc.ChannelOptions {
  return buildChannelOptions();
}

/**
 * Client for the PromptService gRPC API.
 *
 * Registers the dominion resolver on construction, loads the game.proto
 * definition, and creates a service-specific gRPC client that resolves
 * the prompt service endpoint via the dominion URI scheme.
 *
 * The optional `client` parameter allows dependency injection of a mock
 * client for testing without a live gRPC connection.
 */
export class PromptClient {
  private client: grpc.Client;

  /**
   * @param client Optional pre-configured gRPC client (for testing).
   *
   * `registerDominionResolver` is only invoked on the real-construction path
   * (no injected client): an injected client already owns its channel, so it
   * neither needs the dominion URI resolver nor the proto-loader/fs side
   * effects. This lets DI-seamed tests run without module-level mocks of
   * `@dominion/common-js-grpc-resolver` / `@grpc/proto-loader` (FR-009).
   */
  constructor(client?: grpc.Client) {
    if (client) {
      this.client = client;
    } else {
      registerDominionResolver();

      const packageDefinition = protoLoader.loadSync(
        PROTO_PATH,
        PROTO_OPTIONS,
      );
      const proto = grpc.loadPackageDefinition(
        packageDefinition,
      ) as Record<string, unknown>;
      const promptSvc = (proto as any).projects.game.PromptService;
      this.client = new promptSvc(
        PROMPT_SERVICE_TARGET,
        buildClientCredentials(),
        buildChannelOptions(),
      );
    }
  }

  /**
   * Fetch a template's TeamProfile by name.
   *
   * Calls the `GetTeamProfile` RPC on the prompt service and extracts the
   * player/planner model specs from the response's `oneof spec.saolei`
   * (SaoleiProfile). `saolei` is the only known template variant today
   * (specs/031-team-template-mode/contracts/api-contract.md §3.5); a response
   * whose oneof is unset or names a different variant is a contract
   * violation and throws (no silent fallback — directive: typed oneof, no
   * implicit rules).
   *
   * @param template   The template path segment (e.g. `"saolei"`).
   * @param profileName The TeamProfile id (the `{profile}` path segment).
   * @returns The player/planner model specs.
   * @throws {grpc.ServiceError} Propagates gRPC errors from the service.
   *   A missing profile results in NOT_FOUND (code 5).
   * @throws {Error} When the response's oneof spec is not `saolei`.
   */
  async getTeamProfile(
    template: string,
    profileName: string,
  ): Promise<TeamProfileResult> {
    return new Promise<TeamProfileResult>((resolve, reject) => {
      const deadline = new Date();
      deadline.setSeconds(deadline.getSeconds() + 10);

      (this.client as any).getTeamProfile(
        { name: `templates/${template}/profiles/${profileName}` },
        new grpc.Metadata({ waitForReady: true }),
        { deadline },
        (err: grpc.ServiceError | null, response: any) => {
          if (err) {
            reject(err);
            return;
          }
          // proto-loader `oneofs: true` populates the `spec` discriminator
          // only during (de)serialization; outbound raw responses carry it
          // as the oneof case name (`"saolei"`) with the variant on the
          // matching field.
          if (response.spec !== "saolei" || !response.saolei) {
            reject(
              new Error(
                `TeamProfile ${template}/${profileName}: oneof spec must be saolei (got ${String(response.spec)})`,
              ),
            );
            return;
          }
          resolve({
            playerModel: response.saolei.playerModel ?? "",
            plannerModel: response.saolei.plannerModel ?? "",
            // FR-034: base prompts are optional — empty string = unset =
            // template default base (the graph falls back internally).
            playerPrompt: response.saolei.playerPrompt ?? "",
            plannerPrompt: response.saolei.plannerPrompt ?? "",
          });
        },
      );
    });
  }

  /** Close the underlying gRPC client connection. */
  close(): void {
    this.client.close();
  }
}
