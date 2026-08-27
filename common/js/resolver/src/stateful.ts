import { createDeployClient } from "./deploy-client.js";
import {
  validateServiceApp,
  parseDominionEnvironment,
  buildResourceName,
} from "./environment.js";
import { filterEndpoints } from "./endpoint-filter.js";
import {
  ServiceNotStatefulError,
  StatefulInstanceNotFoundError,
  StatefulInstanceNoReadyEndpointsError,
} from "./errors.js";
import { parseTarget } from "./target.js";
import type { Target } from "./target.js";
import type { ResolverConfig, StatefulEndpointResolver } from "./types.js";

const DEFAULT_DEPLOY_BASE_URL = "http://infra.liukexin.com";

/**
 * Creates a `StatefulEndpointResolver` that resolves a single ordinal
 * instance of a stateful service via the deploy HTTP API.
 *
 * Behavior pipeline:
 *  1. Parse target (if string) via `parseTarget`.
 *  2. Validate SERVICE_APP via `validateServiceApp`.
 *  3. Parse environment via `parseDominionEnvironment`.
 *  4. Build resource name via `buildResourceName`.
 *  5. Fetch endpoints via deploy client.
 *  6. Check `isStateful === true` — throw `ServiceNotStatefulError` if not.
 *  7. Find instance with matching `index` — throw `StatefulInstanceNotFoundError` if missing.
 *  8. Filter instance endpoints via `filterEndpoints`.
 *  9. Throw `StatefulInstanceNoReadyEndpointsError` if filtered result is empty.
 * 10. Return filtered endpoints.
 */
export function createStatefulResolver(
  config?: ResolverConfig,
): StatefulEndpointResolver {
  const deployBaseUrl = config?.deployBaseUrl ?? DEFAULT_DEPLOY_BASE_URL;
  const doFetch = config?.fetch ?? globalThis.fetch;
  const env = config?.env ?? process.env;

  const client = createDeployClient({
    deployBaseUrl,
    fetch: doFetch,
    requestTimeoutMs: config?.requestTimeoutMs,
  });

  return {
    async resolveInstance(
      target: string | Target,
      instance: number,
    ): Promise<string[]> {
      const parsed: Target =
        typeof target === "string" ? parseTarget(target) : target;

      validateServiceApp(parsed, env);

      const dominionEnv = parseDominionEnvironment(env);
      const resourceName = buildResourceName(dominionEnv, parsed);
      const serviceEndpoints = await client.getServiceEndpoints(resourceName);

      if (serviceEndpoints.isStateful !== true) {
        throw new ServiceNotStatefulError(
          `service ${JSON.stringify(parsed.service)} is not stateful`,
        );
      }

      const instanceData = serviceEndpoints.statefulInstances.find(
        (inst) => inst.index === instance,
      );
      if (instanceData === undefined) {
        throw new StatefulInstanceNotFoundError(
          `stateful instance ${instance} not found for service ${JSON.stringify(parsed.service)}`,
        );
      }

      const filtered = filterEndpoints(
        instanceData.endpoints,
        parsed.port,
      );

      if (filtered.length === 0) {
        throw new StatefulInstanceNoReadyEndpointsError(
          `stateful instance ${instance} has no ready endpoints for service ${JSON.stringify(parsed.service)}`,
        );
      }

      return filtered;
    },
  };
}
