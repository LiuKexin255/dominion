# Quickstart: Validate gRPC-JS Service Discovery

## Prerequisites

- Work from repository root: `/mnt/code/dominion`.
- Read `.specify/memory/constitution.md`, root `README.md`, and `style/README.md` before implementation.
- Use Bazel-managed tools only.

## Expected package setup

The implementation should create `common/js/grpc/resolver/` with the same package pattern used by `common/js/grpc/otel/` and `common/js/logs/`:

```text
common/js/grpc/resolver/
├── BUILD.bazel
├── package.json
├── run_vitest.mjs
├── tsconfig.json
└── src/
```

The package manifest should use catalog references, for example:

```json
{
  "dependencies": {
    "@grpc/grpc-js": "catalog:"
  },
  "devDependencies": {
    "@types/node": "catalog:",
    "typescript": "catalog:",
    "vitest": "catalog:"
  }
}
```

## Validation scenario 1: Direct resolver call

> **Named-port targets are not supported.** An earlier revision resolved a
> named port (`:grpc`) via a deploy-response `ports` map. Named-port support
> was REMOVED because the grpc-js resolver cannot identify the name part of a
> target, so `parseTarget` rejects non-numeric ports
> (`common/js/resolver/src/target.ts:28-29` "Named ports are not supported").
> Targets use numeric ports; endpoints are published on the target port
> directly (no `ports` remap).

Create a Vitest case that injects fake environment and fake fetch values:

1. Set injected env to `{ DOMINION_ENVIRONMENT: "dev.alpha" }`.
2. Resolve `myapp/myservice:50051`.
3. Assert the fake fetch receives:
   `GET http://infra.liukexin.com/v1/deploy/scopes/dev/environments/alpha/apps/myapp/services/myservice/endpoints`.
4. Return deploy JSON containing `endpoints: ["10.0.0.1:50051", "10.0.0.2:50051"]`.
5. Expect the resolver to return `["10.0.0.1:50051", "10.0.0.2:50051"]`.

## Validation scenario 2: grpc-js resolver update

Create a Vitest case around the grpc-js integration seam:

1. Register or instantiate the dominion grpc-js resolver through the `@grpc/grpc-js` `experimental` resolver interface for `dominion:///myapp/myservice:50051`.
2. Inject a fake deploy resolver returning `["10.0.0.1:50051", "10.0.0.2:50051"]`.
3. Assert grpc-js receives both backend addresses as `Endpoint[]`, one endpoint per `host:port` backend.
4. Trigger a refresh returning the same addresses and assert no duplicate update is published.
5. Trigger a refresh failure and assert the previous address list remains active while the error is reported.
6. Assert listener publication is asynchronous and does not happen inline with constructor or `updateResolution()`.

## Validation scenario 3: Stateful instance resolution

Create a Vitest case for `dominion-stateful:///myapp/myservice:50051?instance=1` (numeric port — named ports are not supported; see scenario 1 note):

1. Fake deploy JSON marks `isStateful: true`.
2. Include instances with indices `0`, `1`, and `2`.
3. Instance `1` has endpoint `10.0.0.11:50051` (published on the target port directly).
4. Expect only `["10.0.0.11:50051"]`.
5. Add negative cases for non-stateful service, missing instance, and no ready endpoints.

## Style validation: JS-native migration

During code review, reject these Go-style migration artifacts:

- `WithX` option-function APIs for public configuration.
- `(value, err)` return tuples or exported sentinel error constants.
- `context` parameters in public JS methods.
- Goroutine/channel/ticker abstractions copied into TypeScript.
- Go package names or struct-style names where idiomatic JS names are clearer.

Prefer these JS-native forms:

- Plain `ResolverConfig` objects.
- Promise rejection with descriptive error classes.
- `async`/`await`.
- Injected `fetch`, environment, and scheduler dependencies for tests.
- grpc-js scheme registration and service config for load balancing.

## Commands

After implementation, run package validation first:

```bash
bazel run //:gazelle common/js/grpc/resolver
bazel build //common/js/grpc/resolver:lib
bazel test //common/js/grpc/resolver:lib_test
```

Then run repository verification:

```bash
bazel build //...
bazel test //...
```

If a later task introduces a large-test plan, execute it through the `testplan` skill and document any deployment blocker.
