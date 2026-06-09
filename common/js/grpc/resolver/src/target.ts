import { InvalidTargetError } from "./errors";

/**
 * Discriminated union for endpoint port selection.
 *
 * - `{ kind: "number"; port: number }` — port is in `0..65535` and matches
 *   endpoint port exactly.
 * - `{ kind: "name"; name: string }` — name uses DNS‑label syntax and
 *   resolves through `ServiceEndpoints.ports`.
 */
export type PortSelector =
  | { kind: "number"; port: number }
  | { kind: "name"; name: string };

/**
 * Parsed dominion service target.
 */
export interface Target {
  app: string;
  service: string;
  port: PortSelector;
}

const DOMINION_SCHEME_PREFIX = "dominion:///";
const MAX_PORT = 65535;
const DNS_LABEL_PATTERN = /^[a-z][a-z0-9-]*$/;

/**
 * Parses a dominion target string of the form `app/service:port`.
 *
 * Accepts:
 * - Direct format: `app/service:port`
 * - Dominion scheme: `dominion:///app/service:port`
 *
 * Whitespace around each segment is trimmed.
 *
 * Throws `InvalidTargetError` for invalid formats, missing segments,
 * unsupported URI schemes, out-of-range numeric ports (0–65535),
 * or invalid named port syntax (must be a DNS label: lowercase letter
 * followed by lowercase letters, digits, or hyphens).
 */
export function parseTarget(raw: string): Target {
  let trimmed = raw.trim();
  if (trimmed === "") {
    throw new InvalidTargetError(
      `invalid target ${JSON.stringify(raw)}: want app/service:port`,
    );
  }

  if (trimmed.startsWith(DOMINION_SCHEME_PREFIX)) {
    trimmed = trimmed.slice(DOMINION_SCHEME_PREFIX.length);
  } else if (trimmed.includes("://")) {
    throw new InvalidTargetError(
      `invalid target ${JSON.stringify(raw)}: want app/service:port or dominion:///app/service:port`,
    );
  }

  const colonIdx = trimmed.indexOf(":");
  let pathPart: string;
  let portPart: string | undefined;
  let hasPort = false;

  if (colonIdx !== -1) {
    pathPart = trimmed.slice(0, colonIdx);
    portPart = trimmed.slice(colonIdx + 1);
    hasPort = true;
  } else {
    pathPart = trimmed;
  }

  const slashIdx = pathPart.indexOf("/");
  if (slashIdx === -1) {
    throw new InvalidTargetError(
      `invalid target ${JSON.stringify(raw)}: want app/service:port`,
    );
  }

  const appPart = pathPart.slice(0, slashIdx).trim();
  const servicePart = pathPart.slice(slashIdx + 1).trim();
  const trimmedPortPart = (portPart ?? "").trim();

  if (appPart === "" || servicePart === "" || servicePart.includes("/")) {
    throw new InvalidTargetError(
      `invalid target ${JSON.stringify(raw)}: want app/service:port`,
    );
  }

  if (trimmedPortPart === "") {
    if (hasPort) {
      throw new InvalidTargetError(
        `invalid target ${JSON.stringify(raw)}: port must be numeric or a valid DNS label`,
      );
    }
    throw new InvalidTargetError(
      `invalid target ${JSON.stringify(raw)}: port is required`,
    );
  }

  if (/^-?\d+$/.test(trimmedPortPart)) {
    const numericPort = parseInt(trimmedPortPart, 10);
    if (numericPort < 0 || numericPort > MAX_PORT) {
      throw new InvalidTargetError(
        `invalid target ${JSON.stringify(raw)}: port out of range`,
      );
    }
    return {
      app: appPart,
      service: servicePart,
      port: { kind: "number", port: numericPort },
    };
  }

  if (!DNS_LABEL_PATTERN.test(trimmedPortPart)) {
    throw new InvalidTargetError(
      `invalid target ${JSON.stringify(raw)}: port must be numeric or a valid DNS label`,
    );
  }

  return {
    app: appPart,
    service: servicePart,
    port: { kind: "name", name: trimmedPortPart },
  };
}
