# Quickstart: Validate JavaScript Runtime Packaging and Idiomatic Common APIs

## 1. Verify no removed logging event package remains

```bash
rg "@dominion/common-js-logs-event|eventString|eventInt|eventBool|eventAny" common/js experimental/ts/grpc_hello_world
```

Expected: no active source usages.

## 2. Build and test common JS packages

```bash
bazel build //common/js/...
bazel test //common/js/...
```

Expected: all retained common JS packages build and tests pass.

## 3. Build the TypeScript gRPC service image artifact

```bash
bazel build //experimental/ts/grpc_hello_world:cmd_image
```

Expected: image artifact builds without missing runtime dependency packaging errors.

## 4. Validate packaged runtime resolution

Run the implementation-provided local package smoke test or equivalent artifact startup check.

Expected: the packaged service entrypoint starts or imports without `MODULE_NOT_FOUND` errors for common JS packages or their third-party dependencies.

## 5. Validate testplan definition

```bash
guitar validate experimental/ts/grpc_hello_world/testplan/interface_test.yaml
```

Expected: validation passes.

## 6. Execute deployment acceptance when infrastructure is reachable

```bash
guitar run experimental/ts/grpc_hello_world/testplan/interface_test.yaml
```

Expected: testplan passes and structured logs are queryable. If deployment infrastructure is unavailable, record the external blocker and residual risk.

## 7. Final repository verification

```bash
bazel build //...
bazel test //...
```

Expected: full repository build and test pass.
