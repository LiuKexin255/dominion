import { InvalidTargetError } from "./errors.js";

/**
 * Numeric port selector — port must be an integer in `0..65535`.
 */
export type PortSelector = { kind: "number"; port: number };

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

/**
 * Parses a dominion target string of the form `app/service:port`.
 *
 * Accepts:
 * - Direct format: `app/service:port`
 * - Dominion scheme: `dominion:///app/service:port`
 *
 * The port must be a non-negative integer in the range 0–65535.
 * Named ports (e.g. `:grpc`) are not supported — callers must
 * resolve the port number themselves before constructing the target.
 *
 * Throws `InvalidTargetError` for invalid formats.
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
        `invalid target ${JSON.stringify(raw)}: port must be numeric`,
      );
    }
    throw new InvalidTargetError(
      `invalid target ${JSON.stringify(raw)}: port is required`,
    );
  }

  if (!/^\d+$/.test(trimmedPortPart)) {
    throw new InvalidTargetError(
      `invalid target ${JSON.stringify(raw)}: port must be numeric, got ${JSON.stringify(trimmedPortPart)}`,
    );
  }

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
