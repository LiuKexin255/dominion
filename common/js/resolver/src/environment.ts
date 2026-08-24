import type { Target } from "./target.js";
import type { DominionEnvironment } from "./types.js";
import {
  MissingEnvironmentError,
  InvalidEnvironmentError,
  InvalidTargetError,
} from "./errors.js";

const DOMINION_ENVIRONMENT_KEY = "DOMINION_ENVIRONMENT";
const SERVICE_APP_KEY = "SERVICE_APP";

/**
 * Parses the `DOMINION_ENVIRONMENT` variable in `scope.envName` format.
 *
 * Splits on the FIRST `.` only, so environment names may contain dots
 * (e.g. `prod.us-east-1.my-cluster` → scope=`prod`, environment=`us-east-1.my-cluster`).
 *
 * @param env - Injected environment map (not `process.env`).
 * @returns A `DominionEnvironment` with `scope` and `environment`.
 * @throws {MissingEnvironmentError} if the variable is unset or empty.
 * @throws {InvalidEnvironmentError} if the format lacks a `.` or either part is empty.
 */
export function parseDominionEnvironment(
  env: Record<string, string | undefined>,
): DominionEnvironment {
  const raw = env[DOMINION_ENVIRONMENT_KEY];
  if (raw === undefined || raw === "") {
    throw new MissingEnvironmentError(
      `missing required env ${DOMINION_ENVIRONMENT_KEY}`,
    );
  }

  const dotIdx = raw.indexOf(".");
  if (dotIdx === -1) {
    throw new InvalidEnvironmentError(
      `invalid ${DOMINION_ENVIRONMENT_KEY} ${JSON.stringify(raw)}: want scope.envName`,
    );
  }

  const scope = raw.slice(0, dotIdx);
  const environment = raw.slice(dotIdx + 1);

  if (scope === "" || environment === "") {
    throw new InvalidEnvironmentError(
      `invalid ${DOMINION_ENVIRONMENT_KEY} ${JSON.stringify(raw)}: want scope.envName`,
    );
  }

  return { scope, environment };
}

/**
 * Validates that `target.app` matches the `SERVICE_APP` environment variable.
 *
 * Mirrors the Go `loadEnvironment` check in `common/gopkg/solver/env.go`.
 *
 * @param target - The parsed deployment target.
 * @param env - Injected environment map (not `process.env`).
 * @throws {MissingEnvironmentError} if `SERVICE_APP` is unset or empty.
 * @throws {InvalidTargetError} if `target.app` does not match `SERVICE_APP`.
 */
export function validateServiceApp(
  target: Target,
  env: Record<string, string | undefined>,
): void {
  const serviceApp = env[SERVICE_APP_KEY];
  if (serviceApp === undefined || serviceApp === "") {
    throw new MissingEnvironmentError(
      `missing required env ${SERVICE_APP_KEY}`,
    );
  }

  if (target.app !== serviceApp) {
    throw new InvalidTargetError(
      `target app ${JSON.stringify(target.app)} does not match ${SERVICE_APP_KEY} ${JSON.stringify(serviceApp)}`,
    );
  }
}

/**
 * Builds a deploy API resource name from a `DominionEnvironment` and `Target`.
 *
 * Format: `deploy/scopes/{scope}/environments/{environment}/apps/{app}/services/{service}/endpoints`
 *
 * @param env - Parsed dominion environment.
 * @param target - Parsed deployment target.
 * @returns The resource path string.
 */
export function buildResourceName(
  env: DominionEnvironment,
  target: Target,
): string {
  return `deploy/scopes/${env.scope}/environments/${env.environment}/apps/${target.app}/services/${target.service}/endpoints`;
}
