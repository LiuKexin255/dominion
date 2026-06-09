import { PortSelector } from "./target";
import { InvalidTargetError } from "./errors";

/** Regex to capture a trailing port number in a `host:port` string. */
const PORT_PATTERN = /:(\d+)$/;

/**
 * Extracts the host part from a `host:port` endpoint string.
 *
 * Handles both IPv4 (`10.0.0.1:50051`) and IPv6 (`[::1]:50051`) formats
 * by finding the last colon separator.
 */
function extractHost(endpoint: string): string {
  const idx = endpoint.lastIndexOf(":");
  return endpoint.slice(0, idx);
}

/**
 * Extracts the numeric port string from a `host:port` endpoint, or
 * `undefined` if no valid trailing port is found.
 */
function extractPort(endpoint: string): string | undefined {
  const match = PORT_PATTERN.exec(endpoint);
  return match?.[1];
}

/**
 * Filters and transforms a list of `host:port` endpoints according to the
 * given port selector.
 *
 * **Numeric port** (`{ kind: "number"; port: N }`):
 * Returns endpoints whose port exactly equals `N`. The original endpoint
 * string is preserved.
 *
 * **Named port** (`{ kind: "name"; name: "grpc" }`):
 * Looks up the numeric port from the optional `ports` map, then replaces
 * the port in every endpoint with the resolved number (keeping the host).
 *
 * Results are deduplicated and sorted lexicographically.
 *
 * @param endpoints - Array of `host:port` strings.
 * @param port      - Port selector (numeric or named).
 * @param ports     - Optional mapping from port name to port number.
 * @returns Sorted, unique `host:port` array.
 * @throws {InvalidTargetError} When a named port is not found in `ports`.
 */
export function filterEndpoints(
  endpoints: string[],
  port: PortSelector,
  ports?: Record<string, number>,
): string[] {
  if (port.kind === "number") {
    return filterByNumericPort(endpoints, port.port);
  }

  // Named port
  if (port.kind === "name") {
    const resolvedPort = ports?.[port.name];
    if (resolvedPort === undefined) {
      throw new InvalidTargetError(
        `named port "${port.name}" not found in service endpoints`,
      );
    }
    return filterByNamedPort(endpoints, resolvedPort);
  }

  return [];
}

/**
 * Filters endpoints whose trailing port matches the given numeric port.
 * Preserves the original endpoint string.
 */
function filterByNumericPort(endpoints: string[], port: number): string[] {
  const portStr = String(port);
  const seen = new Set<string>();

  for (const ep of endpoints) {
    if (extractPort(ep) === portStr) {
      seen.add(ep);
    }
  }

  return [...seen].sort();
}

/**
 * Replaces the port in every endpoint with the resolved named port.
 * Extracts the host from each endpoint and joins with the new port.
 */
function filterByNamedPort(endpoints: string[], port: number): string[] {
  const portStr = String(port);
  const seen = new Set<string>();

  for (const ep of endpoints) {
    seen.add(`${extractHost(ep)}:${portStr}`);
  }

  return [...seen].sort();
}
