import type { Target } from "./target.js";
import type { ResolverConfig, EndpointResolver } from "./types.js";
import { parseTarget } from "./target.js";
import {
  validateServiceApp,
  parseDominionEnvironment,
  buildResourceName,
} from "./environment.js";
import { createDeployClient } from "./deploy-client.js";
import { filterEndpoints } from "./endpoint-filter.js";

/**
 * Creates a stateless `EndpointResolver` that resolves dominion targets
 * to sorted, unique `host:port` endpoint strings.
 *
 * Pipeline per `resolve()` call:
 *   1. Parse target string (or accept `Target` directly).
 *   2. Validate `SERVICE_APP` matches `target.app`.
 *   3. Parse `DOMINION_ENVIRONMENT` into scope + environment.
 *   4. Build deploy API resource name.
 *   5. Fetch endpoints from deploy service.
 *   6. Filter endpoints by port selector.
 *   7. Return sorted, unique endpoint array.
 *
 * All errors propagate without wrapping.
 */
export function createResolver(config?: ResolverConfig): EndpointResolver {
  const deployBaseUrl = config?.deployBaseUrl ?? "http://infra.liukexin.com";
  const doFetch = config?.fetch ?? globalThis.fetch;
  const env = config?.env ?? process.env;

  const deployClient = createDeployClient({
    deployBaseUrl,
    fetch: doFetch,
    requestTimeoutMs: config?.requestTimeoutMs,
  });

  return {
    async resolve(targetInput: string | Target): Promise<string[]> {
      const target: Target =
        typeof targetInput === "string" ? parseTarget(targetInput) : targetInput;

      validateServiceApp(target, env);
      const dominionEnv = parseDominionEnvironment(env);
      const resourceName = buildResourceName(dominionEnv, target);
      const serviceEndpoints = await deployClient.getServiceEndpoints(resourceName);

      return filterEndpoints(
        serviceEndpoints.endpoints,
        target.port,
      );
    },
  };
}
