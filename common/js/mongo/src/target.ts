/** Default mongo port applied when a target omits one. */
export const MONGO_DEFAULT_PORT = 27017;

const DOMINION_SCHEME_PREFIX = "dominion:///";
const MAX_PORT = 65535;

/** The `app`/`service`/`port` parts of a dominion mongo target. */
export interface MongoTarget {
  app: string;
  service: string;
  port: number;
}

/**
 * Parses a dominion mongo target into its normalized parts.
 *
 * Two input forms are accepted (specs/067-redis-infra-support/contracts/
 * common-db-libs.md §3): the bare `app/name` form, whose port defaults to
 * {@link MONGO_DEFAULT_PORT}, and the `dominion:///app/service:port` form used
 * by existing JS callers. Both normalize to `app/service:port`, which is what
 * `@dominion/common-js-resolver` requires (it cannot infer a default port).
 */
export function parseMongoTarget(raw: string): MongoTarget {
  const trimmed = raw.trim();
  if (trimmed === "") {
    throw invalidTarget(raw);
  }

  let body = trimmed;
  if (body.startsWith(DOMINION_SCHEME_PREFIX)) {
    body = body.slice(DOMINION_SCHEME_PREFIX.length);
  } else if (body.includes("://")) {
    throw invalidTarget(raw);
  }

  const slash = body.indexOf("/");
  if (slash === -1) {
    throw invalidTarget(raw);
  }

  const app = body.slice(0, slash).trim();
  const rest = body.slice(slash + 1);

  const colon = rest.lastIndexOf(":");
  let service = rest.trim();
  let port = MONGO_DEFAULT_PORT;
  if (colon !== -1) {
    const portPart = rest.slice(colon + 1).trim();
    service = rest.slice(0, colon).trim();
    if (!/^\d+$/.test(portPart)) {
      throw invalidTarget(raw);
    }
    port = Number(portPart);
  }

  if (
    app === "" ||
    service === "" ||
    service.includes("/") ||
    service.includes(":") ||
    port <= 0 ||
    port > MAX_PORT
  ) {
    throw invalidTarget(raw);
  }

  return { app, service, port };
}

/** Renders a parsed target as the resolver's `app/service:port` form. */
export function resolveTarget(target: MongoTarget): string {
  return `${target.app}/${target.service}:${target.port}`;
}

function invalidTarget(raw: string): Error {
  return new Error(
    `invalid mongo target ${JSON.stringify(raw)}: want "app/name" or "dominion:///app/service:port"`,
  );
}
