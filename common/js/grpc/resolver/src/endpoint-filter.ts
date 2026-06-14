import { PortSelector } from "./target";

const PORT_PATTERN = /:(\d+)$/;

/**
 * Filters endpoints whose trailing port matches the given numeric port.
 * Preserves the original endpoint string. Deduplicates and sorts results.
 */
export function filterEndpoints(
  endpoints: string[],
  port: PortSelector,
): string[] {
  const portStr = String(port.port);
  const seen = new Set<string>();

  for (const ep of endpoints) {
    const match = PORT_PATTERN.exec(ep);
    if (match?.[1] === portStr) {
      seen.add(ep);
    }
  }

  return [...seen].sort();
}
