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
export const PROMPT_SERVICE_TARGET = "dominion:///game/prompt:grpc";

const TLS_CA_CERT = "/etc/tls/ca.crt";

/** Return type for PromptClient.getProfile(). */
export interface ProfileResult {
  model: string;
  systemPrompt: string;
}

function buildClientCredentials(): grpc.ChannelCredentials {
  if (!fs.existsSync(TLS_CA_CERT)) {
    return grpc.credentials.createInsecure();
  }

  const rootCert = fs.readFileSync(TLS_CA_CERT);
  return grpc.credentials.createSsl(rootCert);
}

function buildChannelOptions(): Record<string, unknown> {
  const serverName = process.env.TLS_SERVER_NAME;
  if (serverName && fs.existsSync(TLS_CA_CERT)) {
    return { "grpc.ssl_target_name_override": serverName };
  }
  return {};
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
      (this.client as any).getAgentProfile(
        { agentProfileName: profileName },
        (err: grpc.ServiceError | null, response: any) => {
          if (err) {
            reject(err);
            return;
          }
          resolve({
            model: response.model,
            systemPrompt: response.systemPrompt,
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
