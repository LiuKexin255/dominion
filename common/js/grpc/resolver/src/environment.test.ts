import { describe, expect, it } from "vitest";
import type { Target } from "./target";
import type { DominionEnvironment } from "./types";
import {
  parseDominionEnvironment,
  validateServiceApp,
  buildResourceName,
} from "./environment";
import {
  MissingEnvironmentError,
  InvalidEnvironmentError,
  InvalidTargetError,
} from "./errors";

const ENV_KEY = "DOMINION_ENVIRONMENT";
const APP_KEY = "SERVICE_APP";

// ---------------------------------------------------------------------------
// parseDominionEnvironment
// ---------------------------------------------------------------------------

describe("parseDominionEnvironment", () => {
  it("parses DOMINION_ENVIRONMENT=dev.alpha into scope=dev, environment=alpha", () => {
    const result = parseDominionEnvironment({ [ENV_KEY]: "dev.alpha" });
    expect(result).toEqual<DominionEnvironment>({
      scope: "dev",
      environment: "alpha",
    });
  });

  it("splits on first dot only: prod.us-east-1.my-cluster → {prod, us-east-1.my-cluster}", () => {
    const result = parseDominionEnvironment({
      [ENV_KEY]: "prod.us-east-1.my-cluster",
    });
    expect(result).toEqual<DominionEnvironment>({
      scope: "prod",
      environment: "us-east-1.my-cluster",
    });
  });

  it("throws MissingEnvironmentError when DOMINION_ENVIRONMENT is undefined", () => {
    expect(() => parseDominionEnvironment({})).toThrow(MissingEnvironmentError);
  });

  it("throws MissingEnvironmentError when DOMINION_ENVIRONMENT is empty string", () => {
    expect(() => parseDominionEnvironment({ [ENV_KEY]: "" })).toThrow(
      MissingEnvironmentError,
    );
  });

  it("throws InvalidEnvironmentError when DOMINION_ENVIRONMENT has no dot", () => {
    expect(() =>
      parseDominionEnvironment({ [ENV_KEY]: "justascope" }),
    ).toThrow(InvalidEnvironmentError);
  });

  it("throws InvalidEnvironmentError when scope is empty (dot at start)", () => {
    expect(() => parseDominionEnvironment({ [ENV_KEY]: ".envname" })).toThrow(
      InvalidEnvironmentError,
    );
  });

  it("throws InvalidEnvironmentError when environment is empty (dot at end)", () => {
    expect(() => parseDominionEnvironment({ [ENV_KEY]: "scope." })).toThrow(
      InvalidEnvironmentError,
    );
  });

  it("includes the raw value in the error message on invalid format", () => {
    const e = () => parseDominionEnvironment({ [ENV_KEY]: "bad" });
    expect(e).toThrow(InvalidEnvironmentError);
    expect(e).toThrow(/"bad"/);
  });
});

// ---------------------------------------------------------------------------
// validateServiceApp
// ---------------------------------------------------------------------------

describe("validateServiceApp", () => {
  const target: Target = {
    app: "myapp",
    service: "myservice",
    port: { kind: "number", port: 50051 },
  };

  it("passes when target.app matches SERVICE_APP", () => {
    expect(() =>
      validateServiceApp(target, { [APP_KEY]: "myapp" }),
    ).not.toThrow();
  });

  it("throws MissingEnvironmentError when SERVICE_APP is undefined", () => {
    expect(() => validateServiceApp(target, {})).toThrow(MissingEnvironmentError);
  });

  it("throws MissingEnvironmentError when SERVICE_APP is empty string", () => {
    expect(() => validateServiceApp(target, { [APP_KEY]: "" })).toThrow(
      MissingEnvironmentError,
    );
  });

  it("throws InvalidTargetError when target.app does not match SERVICE_APP", () => {
    expect(() =>
      validateServiceApp(target, { [APP_KEY]: "otherapp" }),
    ).toThrow(InvalidTargetError);
  });

  it("throws InvalidTargetError with descriptive message on mismatch", () => {
    const e = () => validateServiceApp(target, { [APP_KEY]: "otherapp" });
    expect(e).toThrow(InvalidTargetError);
    expect(e).toThrow(/myapp.*SERVICE_APP.*otherapp/);
  });
});

// ---------------------------------------------------------------------------
// buildResourceName
// ---------------------------------------------------------------------------

describe("buildResourceName", () => {
  it("constructs the correct deploy API path", () => {
    const env: DominionEnvironment = { scope: "dev", environment: "alpha" };
    const target: Target = {
      app: "myapp",
      service: "myservice",
      port: { kind: "number", port: 50051 },
    };
    const result = buildResourceName(env, target);
    expect(result).toBe(
      "deploy/scopes/dev/environments/alpha/apps/myapp/services/myservice/endpoints",
    );
  });

  it("handles multi-dot environment names", () => {
    const env: DominionEnvironment = {
      scope: "prod",
      environment: "us-east-1.my-cluster",
    };
    const target: Target = {
      app: "api",
      service: "grpc",
      port: { kind: "name", name: "grpc" },
    };
    const result = buildResourceName(env, target);
    expect(result).toBe(
      "deploy/scopes/prod/environments/us-east-1.my-cluster/apps/api/services/grpc/endpoints",
    );
  });
});
