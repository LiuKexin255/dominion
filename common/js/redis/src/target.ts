/** Default redis port applied to `app/name` targets. */
export const REDIS_DEFAULT_PORT = 6379;

/** The `app`/`name` halves of a dominion redis target. */
export interface RedisTarget {
  app: string;
  service: string;
}

/**
 * Parses a dominion redis target of the form `app/name`.
 *
 * The shared JS resolver (`@dominion/common-js-resolver`) requires an explicit
 * numeric port; {@link resolveTarget} appends the default redis port.
 */
export function parseRedisTarget(raw: string): RedisTarget {
  const trimmed = raw.trim();
  const slash = trimmed.indexOf("/");
  if (slash === -1) {
    throw new Error(`invalid redis target ${JSON.stringify(raw)}: want "app/name"`);
  }

  const app = trimmed.slice(0, slash).trim();
  const service = trimmed.slice(slash + 1).trim();
  if (app === "" || service === "" || service.includes("/") || service.includes(":")) {
    throw new Error(`invalid redis target ${JSON.stringify(raw)}: want "app/name"`);
  }

  return { app, service };
}

/** Renders a parsed target as the resolver's `app/service:port` form. */
export function resolveTarget(target: RedisTarget): string {
  return `${target.app}/${target.service}:${REDIS_DEFAULT_PORT}`;
}
