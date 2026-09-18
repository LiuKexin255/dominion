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
import { info, error } from "@dominion/common-js-logs";

/**
 * Path to game.proto, relative to the compiled src/ directory.
 *
 * Exported so sibling gRPC clients (e.g. `memory-client.ts`, spec 039 T014 —
 * `specs/039-planner-memory-calibration/contracts/memory-mcp-contract.md` §3)
 * load the SAME proto definition with the SAME options instead of
 * duplicating the path/loader config.
 */
export const PROTO_PATH = path.join(
  import.meta.dirname,
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
  includeDirs: [path.join(import.meta.dirname, "..")],
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

// Deliberately NO app-level keepalive PINGs on these unary clients — this
// mirrors grpc-go's `ClientDefault()` (common/gopkg/grpc/default.go:26): the
// Go side reserves HTTP/2 keepalive for long-lived streams, paired with
// server-side enforcement relaxation (common/gopkg/grpc/keepalive.go
// WithLongLivedClientKeepalive / WithLongLivedServerKeepalive). The unary
// prompt/memory servers run grpc-go's DEFAULT enforcement policy (MinTime=5min,
// PermitWithoutStream=false), so a keepalive-enabled client's idle PINGs are
// answered with GOAWAY "excess pings" and the connection is torn down
// repeatedly, producing DEADLINE_EXCEEDED "Waiting for LB pick" on the
// agent→prompt call. Node lacks TCP-level SO_KEEPALIVE (grpc-go enables it;
// https://github.com/grpc/grpc-go/issues/6250), but for unary calls a dead
// connection is detected by the per-RPC deadline + reconnect, which is
// exactly grpc-go's unary behavior.
//
// max_reconnect_backoff_ms: cap the subchannel retry interval (grpc-js
// default max backoff is 120s; 15s bounds how long a dead subchannel is
// retried before giving up the connection).
export const RECONNECT_OPTIONS: grpc.ChannelOptions = {
  "grpc.initial_reconnect_backoff_ms": 1_000,
  "grpc.max_reconnect_backoff_ms": 15_000,
};

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

      // Probe-and-nudge the channel (forces IDLE→CONNECTING; logs state for
      // diagnosis — see probeChannel): a connection drop must not leave the
      // channel permanently stuck in "Waiting for LB pick".
      probeChannel(this.client, "prompt getTeamProfile");

      (this.client as any).getTeamProfile(
        { name: `templates/${template}/profiles/${profileName}` },
        new grpc.Metadata({ waitForReady: true }),
        { deadline },
        (err: grpc.ServiceError | null, response: any) => {
          if (err) {
            error("prompt getTeamProfile failed", {
              template,
              profile: profileName,
              error: err.message,
              code: String(err.code),
              channelState: probeChannel(
                this.client,
                "prompt getTeamProfile (after failure)",
              ),
            });
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

/**
 * Probe a grpc-js channel's connectivity state and nudge it to connect.
 *
 * `getConnectivityState(true)` forces IDLE→CONNECTING — the same nudge the
 * removed warmup() used — so a call on an idle channel actively tries to
 * reconnect instead of silently queueing until the deadline. grpc-js
 * round_robin does not reliably exit IDLE after a connection drop (see the
 * empty-endpoint comment in common/js/grpc/resolver/src/grpc-js-resolver.ts),
 * leaving the channel stuck in "Waiting for LB pick" until the process
 * restarts, so every unary RPC must probe its channel first. Logs the state
 * at info level for diagnosis and returns the state name.
 *
 * @param client The gRPC client whose channel to probe.
 * @param label  Log label identifying the call site.
 * @returns The connectivity state name ("READY"/"IDLE"/...), or a
 *   descriptive string when the channel is unavailable (injected test
 *   client without a channel).
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
