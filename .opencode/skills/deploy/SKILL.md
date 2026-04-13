---
name: deploy
description: Deploy a service to Kubernetes with tools/deploy for interface or smoke testing, then verify and clean up safely.
compatibility: opencode
license: MIT
metadata:
  repo: dominion
  tool: tools/deploy
  workflow: interface-testing
---

## What this skill does

Use this skill when a task needs a real Kubernetes deployment before running interface tests, smoke checks, or other cluster-backed verification.

This repository already provides a deployment CLI in `tools/deploy`. Your job is to use that CLI safely and consistently from the Bazel workspace root.

## When to use me

- The user asks to deploy a service to a k8s cluster.
- The user wants interface tests against a real deployed service.
- The task mentions `deploy.yaml`, `service.yaml`, `tools/deploy`, smoke tests, or cluster verification.

Do **not** use this skill for pure unit tests, code-only changes, or local mocking.

## Required behavior

1. Work from the repository root so `deploy` path resolution works correctly.
2. Read `tools/deploy/README.md` before deploying if the exact command flow is unclear.
3. Prefer `deploy apply [--kubeconfig=...] <deploy.yaml>` over hand-written `kubectl` resource creation.
4. Use a full environment name when issuing `deploy use` or `deploy del` manually.
5. After deployment, run the intended interface test or at least a focused smoke check.
6. If you created an ephemeral environment for this task, clean it up after verification unless the user asked to keep it.

## Tool workflow

### 1) Confirm prerequisites

- Ensure you are inside the Bazel workspace root.
- Confirm there is a target `deploy.yaml` for the service under test.
- Determine whether a kubeconfig path is required.

If `deploy` is not available in `PATH`, install it with:

```bash
bazel run //:deploy_install
```

The installer places `deploy` at `$HOME/.local/bin/deploy` by default.

### 2) Inspect the target deployment definition

Read the target `deploy.yaml` and identify:

- environment name (`name`)
- deployed services
- whether the target already matches the service under test

If the task names a service but not the deploy manifest, search for likely `deploy.yaml` files near that service.

### 3) Build the deploy command

Use one of these forms:

```bash
deploy apply //path/to/deploy.yaml
deploy apply --kubeconfig=/path/to/kubeconfig //path/to/deploy.yaml
```

Notes:

- `apply` reads the full environment name from `deploy.yaml`.
- `apply` automatically creates the environment if it does not already exist.
- `apply` activates the deployed environment on success.
- Relative paths resolve from the current shell working directory.
- `//...` paths resolve from the Bazel workspace root.

### 4) Verify deployment state

After `deploy apply`, verify with at least one of:

```bash
deploy cur
deploy list
```

Then run the interface test or smoke validation relevant to the task. Prefer repository-native test commands over ad-hoc probes.

Examples:

- a dedicated bazel test target for the API package
- a focused integration test command
- a service-specific curl/grpcurl smoke check when no test target exists

### 5) Clean up safely

If you created or used a temporary environment for this task, remove it after testing:

```bash
deploy del alice.dev
deploy del alice.dev --kubeconfig=/path/to/kubeconfig
```

Only delete an environment when one of the following is true:

- you created it in this task
- the user explicitly asked for teardown
- the repository docs or task context clearly mark it as ephemeral

Do **not** delete a shared or long-lived environment just to leave things tidy.

## Safety rules

- Never bypass `tools/deploy` with manual `kubectl apply` unless the task explicitly requires debugging the deploy tool itself.
- Never delete an environment you did not create unless the user asked you to.
- If cluster access fails, surface the exact kubeconfig, context, or permission error.
- If the deploy manifest path is ambiguous, inspect the repo and choose the closest manifest to the requested service before asking the user.
- Keep the user informed whether the environment was deployed, reused, or cleaned up.

## Default execution pattern

When the request is “deploy this service for interface testing”, use this sequence:

1. Locate the relevant `deploy.yaml`.
2. Install `deploy` if needed.
3. Run `deploy apply` with optional `--kubeconfig`.
4. Confirm the active environment with `deploy cur`.
5. Run the intended interface test or smoke check.
6. Tear down only if the environment is ephemeral or the user asked.

## Command snippets

```bash
bazel run //:deploy_install
deploy apply //experimental/grpc_hello_world/deploy.yaml
deploy apply --kubeconfig=/var/snap/microk8s/current/credentials/client.config //experimental/grpc_hello_world/deploy.yaml
deploy cur
deploy del alice.dev
```

## What to report back

Always report:

- which `deploy.yaml` you used
- whether `--kubeconfig` was used
- the resulting active environment
- which interface test or smoke check ran
- whether cleanup happened

If deployment could not proceed, report the blocking prerequisite precisely instead of giving a vague failure summary.
