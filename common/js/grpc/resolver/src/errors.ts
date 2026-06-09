/**
 * Error classes for @dominion/common-js-grpc-resolver.
 *
 * All errors extend Error and include descriptive messages.
 * Callers may use `instanceof` checks.
 */

export class InvalidTargetError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "InvalidTargetError";
  }
}

export class MissingEnvironmentError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "MissingEnvironmentError";
  }
}

export class InvalidEnvironmentError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "InvalidEnvironmentError";
  }
}

export class DeployServiceError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "DeployServiceError";
  }
}

export class ServiceNotFoundError extends DeployServiceError {
  constructor(message: string) {
    super(message);
    this.name = "ServiceNotFoundError";
  }
}

export class ServiceNotStatefulError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ServiceNotStatefulError";
  }
}

export class StatefulInstanceNotFoundError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "StatefulInstanceNotFoundError";
  }
}

export class StatefulInstanceNoReadyEndpointsError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "StatefulInstanceNoReadyEndpointsError";
  }
}
