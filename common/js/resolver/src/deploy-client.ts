import type { FetchLike, ServiceEndpoints, StatefulInstance } from "./types";
import { DeployServiceError, ServiceNotFoundError } from "./errors";

/** Default base URL for the deploy service. */
const DEFAULT_DEPLOY_BASE_URL = "http://infra.liukexin.com";

/** Default timeout for each deploy API request. */
export const DEFAULT_REQUEST_TIMEOUT_MS = 5_000;

/**
 * Client for fetching service endpoints from the deploy HTTP API.
 */
export interface DeployClient {
  getServiceEndpoints(resourceName: string): Promise<ServiceEndpoints>;
}

/**
 * Map a raw JSON object (which may contain proto-style snake_case fields)
 * into a `ServiceEndpoints` instance.
 *
 * Handles both `is_stateful`/`isStateful` and
 * `stateful_instances`/`statefulInstances` naming conventions.
 * Unknown fields are silently ignored.
 */
function mapServiceEndpoints(raw: Record<string, unknown>): ServiceEndpoints {
  const isStateful = (raw.isStateful ?? raw.is_stateful) as boolean;
  const statefulInstances =
    (raw.statefulInstances ?? raw.stateful_instances) as
      | StatefulInstance[]
      | undefined;

  return {
    endpoints: (raw.endpoints ?? []) as string[],
    ports: (raw.ports ?? {}) as Record<string, number>,
    isStateful: isStateful ?? false,
    statefulInstances: statefulInstances ?? [],
  };
}

/**
 * Create a `DeployClient` backed by an injected `fetch` implementation.
 *
 * @param config - Optional configuration overrides.
 * @param config.deployBaseUrl - Base URL of the deploy service.
 *   Defaults to `"http://infra.liukexin.com"`.
 * @param config.fetch - A `FetchLike` function used for HTTP requests.
 *   Defaults to `globalThis.fetch`.
 * @param config.requestTimeoutMs - Per-request timeout in milliseconds.
 *   Defaults to {@link DEFAULT_REQUEST_TIMEOUT_MS}.
 */
export function createDeployClient(config?: {
  deployBaseUrl?: string;
  fetch?: FetchLike;
  requestTimeoutMs?: number;
}): DeployClient {
  const baseUrl = config?.deployBaseUrl ?? DEFAULT_DEPLOY_BASE_URL;
  const doFetch = config?.fetch ?? globalThis.fetch;
  const requestTimeoutMs =
    config?.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;

  return {
    async getServiceEndpoints(resourceName: string): Promise<ServiceEndpoints> {
      const url = `${baseUrl}/v1/${resourceName}`;

      let response: Response;
      try {
        response = await doFetch(url, {
          signal: AbortSignal.timeout(requestTimeoutMs),
        });
      } catch (err: unknown) {
        if (err instanceof Error && err.name === "TimeoutError") {
          throw new DeployServiceError(
            `deploy service request timed out after ${requestTimeoutMs}ms`,
          );
        }
        throw new DeployServiceError(
          `deploy service request failed: ${err instanceof Error ? err.message : String(err)}`,
        );
      }

      if (response.status === 404) {
        throw new ServiceNotFoundError(
          `service not found: ${resourceName}`,
        );
      }

      if (!response.ok) {
        const body = await response.text().catch(() => "");
        throw new DeployServiceError(
          `deploy service returned status ${response.status}: ${body}`,
        );
      }

      const raw = (await response.json()) as Record<string, unknown>;
      return mapServiceEndpoints(raw);
    },
  };
}
