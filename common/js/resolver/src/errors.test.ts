import { describe, it, expect } from "vitest";
import {
  InvalidTargetError,
  MissingEnvironmentError,
  InvalidEnvironmentError,
  DeployServiceError,
  ServiceNotFoundError,
  ServiceNotStatefulError,
  StatefulInstanceNotFoundError,
  StatefulInstanceNoReadyEndpointsError,
} from "./errors";

describe("InvalidTargetError", () => {
  it("constructs with a message", () => {
    const err = new InvalidTargetError("invalid target: app/");
    expect(err.message).toBe("invalid target: app/");
  });

  it("sets name to InvalidTargetError", () => {
    const err = new InvalidTargetError("test");
    expect(err.name).toBe("InvalidTargetError");
  });

  it("is instanceof InvalidTargetError", () => {
    const err = new InvalidTargetError("test");
    expect(err).toBeInstanceOf(InvalidTargetError);
  });

  it("is instanceof Error", () => {
    const err = new InvalidTargetError("test");
    expect(err).toBeInstanceOf(Error);
  });
});

describe("MissingEnvironmentError", () => {
  it("constructs with a message", () => {
    const err = new MissingEnvironmentError("DOMINION_ENVIRONMENT not set");
    expect(err.message).toBe("DOMINION_ENVIRONMENT not set");
  });

  it("sets name to MissingEnvironmentError", () => {
    const err = new MissingEnvironmentError("test");
    expect(err.name).toBe("MissingEnvironmentError");
  });

  it("is instanceof MissingEnvironmentError", () => {
    const err = new MissingEnvironmentError("test");
    expect(err).toBeInstanceOf(MissingEnvironmentError);
  });

  it("is instanceof Error", () => {
    const err = new MissingEnvironmentError("test");
    expect(err).toBeInstanceOf(Error);
  });
});

describe("InvalidEnvironmentError", () => {
  it("constructs with a message", () => {
    const err = new InvalidEnvironmentError("bad env format");
    expect(err.message).toBe("bad env format");
  });

  it("sets name to InvalidEnvironmentError", () => {
    const err = new InvalidEnvironmentError("test");
    expect(err.name).toBe("InvalidEnvironmentError");
  });

  it("is instanceof InvalidEnvironmentError", () => {
    const err = new InvalidEnvironmentError("test");
    expect(err).toBeInstanceOf(InvalidEnvironmentError);
  });

  it("is instanceof Error", () => {
    const err = new InvalidEnvironmentError("test");
    expect(err).toBeInstanceOf(Error);
  });
});

describe("DeployServiceError", () => {
  it("constructs with a message", () => {
    const err = new DeployServiceError("API returned 500");
    expect(err.message).toBe("API returned 500");
  });

  it("sets name to DeployServiceError", () => {
    const err = new DeployServiceError("test");
    expect(err.name).toBe("DeployServiceError");
  });

  it("is instanceof DeployServiceError", () => {
    const err = new DeployServiceError("test");
    expect(err).toBeInstanceOf(DeployServiceError);
  });

  it("is instanceof Error", () => {
    const err = new DeployServiceError("test");
    expect(err).toBeInstanceOf(Error);
  });
});

describe("ServiceNotFoundError", () => {
  it("constructs with a message", () => {
    const err = new ServiceNotFoundError("service not found: svc");
    expect(err.message).toBe("service not found: svc");
  });

  it("sets name to ServiceNotFoundError", () => {
    const err = new ServiceNotFoundError("test");
    expect(err.name).toBe("ServiceNotFoundError");
  });

  it("is instanceof ServiceNotFoundError", () => {
    const err = new ServiceNotFoundError("test");
    expect(err).toBeInstanceOf(ServiceNotFoundError);
  });

  it("is instanceof DeployServiceError", () => {
    const err = new ServiceNotFoundError("test");
    expect(err).toBeInstanceOf(DeployServiceError);
  });

  it("is instanceof Error", () => {
    const err = new ServiceNotFoundError("test");
    expect(err).toBeInstanceOf(Error);
  });
});

describe("ServiceNotStatefulError", () => {
  it("constructs with a message", () => {
    const err = new ServiceNotStatefulError("service is not stateful");
    expect(err.message).toBe("service is not stateful");
  });

  it("sets name to ServiceNotStatefulError", () => {
    const err = new ServiceNotStatefulError("test");
    expect(err.name).toBe("ServiceNotStatefulError");
  });

  it("is instanceof ServiceNotStatefulError", () => {
    const err = new ServiceNotStatefulError("test");
    expect(err).toBeInstanceOf(ServiceNotStatefulError);
  });

  it("is instanceof Error", () => {
    const err = new ServiceNotStatefulError("test");
    expect(err).toBeInstanceOf(Error);
  });
});

describe("StatefulInstanceNotFoundError", () => {
  it("constructs with a message", () => {
    const err = new StatefulInstanceNotFoundError(
      "instance index 5 not found"
    );
    expect(err.message).toBe("instance index 5 not found");
  });

  it("sets name to StatefulInstanceNotFoundError", () => {
    const err = new StatefulInstanceNotFoundError("test");
    expect(err.name).toBe("StatefulInstanceNotFoundError");
  });

  it("is instanceof StatefulInstanceNotFoundError", () => {
    const err = new StatefulInstanceNotFoundError("test");
    expect(err).toBeInstanceOf(StatefulInstanceNotFoundError);
  });

  it("is instanceof Error", () => {
    const err = new StatefulInstanceNotFoundError("test");
    expect(err).toBeInstanceOf(Error);
  });
});

describe("StatefulInstanceNoReadyEndpointsError", () => {
  it("constructs with a message", () => {
    const err = new StatefulInstanceNoReadyEndpointsError(
      "instance 0 has no ready endpoints"
    );
    expect(err.message).toBe("instance 0 has no ready endpoints");
  });

  it("sets name to StatefulInstanceNoReadyEndpointsError", () => {
    const err = new StatefulInstanceNoReadyEndpointsError("test");
    expect(err.name).toBe("StatefulInstanceNoReadyEndpointsError");
  });

  it("is instanceof StatefulInstanceNoReadyEndpointsError", () => {
    const err = new StatefulInstanceNoReadyEndpointsError("test");
    expect(err).toBeInstanceOf(StatefulInstanceNoReadyEndpointsError);
  });

  it("is instanceof Error", () => {
    const err = new StatefulInstanceNoReadyEndpointsError("test");
    expect(err).toBeInstanceOf(Error);
  });
});
