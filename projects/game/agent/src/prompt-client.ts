/**
 * PromptClient — gRPC client for the PromptService.
 *
 * Resolves the prompt service via the dominion resolver and calls the
 * GetAgentProfile RPC to fetch agent configuration profiles.
 */
import * as fs from "node:fs";
import * as grpc from "@grpc/grpc-js";
import * as protoLoader from "@grpc/proto-loader";
import * as path from "node:path";
import {
  registerDominionResolver,
  createDeployClient,
} from "@dominion/common-js-grpc-resolver";

/** Path to game.proto, relative to the compiled src/ directory. */
const PROTO_PATH = path.join(
  __dirname,
  "..",
  "projects", "game", "game.proto",
);

/** Proto loader options MUST match ts_proto_library generation options. */
const PROTO_OPTIONS: protoLoader.Options = {
  longs: String,
  enums: String,
  defaults: true,
  oneofs: true,
  includeDirs: [path.join(__dirname, "..")],
};

/** Dominion resolver target for the prompt service. */
export const PROMPT_SERVICE_TARGET = "dominion:///game/prompt:50051";

const TLS_CA_CERT = "/etc/tls/ca.crt";

/** Ample for TCP + TLS handshake on a healthy peer during startup warmup. */
const DEFAULT_WARMUP_TIMEOUT_MS = 5_000;

/** Return type for PromptClient.getProfile(). */
export interface ProfileResult {
  model: string;
  systemPrompt: string;
  /** Tool names declared on the profile (proto field `tool_names`). */
  toolNames: string[];
}

function buildClientCredentials(): grpc.ChannelCredentials {
  if (!fs.existsSync(TLS_CA_CERT)) {
    return grpc.credentials.createInsecure();
  }

  const rootCert = fs.readFileSync(TLS_CA_CERT);
  return grpc.credentials.createSsl(rootCert);
}

// Keep the channel warm and detect silently-dropped connections, but stay
// within the prompt service's keepalive enforcement policy. The prompt
// server is grpc-go, which by default rejects pings more frequent than
// 5 minutes as "excess pings" and GOAWAYs the connection. Use 5m so the
// PING interval is safely above that threshold, and only send PINGs when
// there is an active RPC to avoid waking idle connections.
const KEEPALIVE_OPTIONS: grpc.ChannelOptions = {
  "grpc.keepalive_time_ms": 300_000,
  "grpc.keepalive_timeout_ms": 10_000,
  "grpc.keepalive_permit_without_calls": 0,
  "grpc.initial_reconnect_backoff_ms": 1_000,
  "grpc.max_reconnect_backoff_ms": 15_000,
};

// grpc-js defaults to pick_first, which pins a single connection; when that
// backend pod restarts the call hangs in "Waiting for LB pick" until reconnect.
// round_robin (matching grpc-go's ClientDefault) connects to every resolved
// endpoint, so a rolling-upgrade pod swap routes around the terminating pod.
const ROUND_ROBIN_SERVICE_CONFIG = JSON.stringify({
  loadBalancingConfig: [{ round_robin: {} }],
});

function buildChannelOptions(): grpc.ChannelOptions {
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
   */
  constructor(client?: grpc.Client) {
    registerDominionResolver();

    if (client) {
      this.client = client;
    } else {
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
   * Fetch an agent profile by name.
   *
   * Calls the `GetAgentProfile` RPC on the prompt service and extracts
   * the `model` and `systemPrompt` fields from the returned `AgentProfile`
   * message.
   *
   * @param profileName - The agent profile name to fetch.
   * @returns The profile's model and system prompt.
   * @throws {grpc.ServiceError} Propagates gRPC errors from the service.
   *   A missing profile results in NOT_FOUND (code 5).
   */
  async getProfile(profileName: string): Promise<ProfileResult> {
    return new Promise<ProfileResult>((resolve, reject) => {
      const deadline = new Date();
      deadline.setSeconds(deadline.getSeconds() + 10);

      (this.client as any).getAgentProfile(
        { name: `prompts/agentProfiles/${profileName}` },
        new grpc.Metadata({ waitForReady: true }),
        { deadline },
        (err: grpc.ServiceError | null, response: any) => {
          if (err) {
            reject(err);
            return;
          }
          resolve({
            model: response.model,
            systemPrompt: response.systemPrompt,
            toolNames: response.toolNames ?? [],
          });
        },
      );
    });
  }

  /**
   * Best-effort pre-warm of the gRPC channel.
   *
   * Forces the otherwise-lazy channel to start connecting and waits until it
   * reaches READY or `timeoutMs` elapses. Eliminates the cold-start window on
   * the first real RPC. Never rejects — a timeout resolves `false` and leaves
   * connection establishment to the next RPC's own deadline.
   *
   * @returns `true` if the channel reached READY, `false` on timeout.
   */
  async warmup(timeoutMs = DEFAULT_WARMUP_TIMEOUT_MS): Promise<boolean> {
    return new Promise<boolean>((resolve) => {
      const channel = this.client.getChannel();
      const deadline = new Date(Date.now() + timeoutMs);

      const step = (): void => {
        const state = channel.getConnectivityState(true);
        if (
          state === grpc.connectivityState.READY ||
          state === grpc.connectivityState.SHUTDOWN
        ) {
          resolve(state === grpc.connectivityState.READY);
          return;
        }
        channel.watchConnectivityState(state, deadline, (err) => {
          if (err) {
            resolve(false);
            return;
          }
          step();
        });
      };
      step();
    });
  }

  /** Close the underlying gRPC client connection. */
  close(): void {
    this.client.close();
  }
}
