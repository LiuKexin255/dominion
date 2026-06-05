# Contract: JS Artifact Runtime Dependencies

## Purpose

`artifact_pkg_js` must package a service with all runtime modules needed to start under Node while allowing the service to declare only direct runtime dependencies.

## Caller Contract

The service package declares:

- its compiled service target
- runtime proto sources, when needed
- direct runtime dependencies imported by service files
- service entrypoint path

The service package does not declare dependencies only imported by shared packages.

## Runtime Package Contract

Workspace runtime packages expose:

- package name
- package metadata
- runtime JavaScript outputs
- declaration outputs
- direct runtime dependencies
- transitive runtime files or enough provider data for the artifact rule to collect them

## Artifact Contract

The produced artifact contains:

- service files under the service root
- proto runtime files under canonical paths
- runtime packages under a Node-resolvable package layout
- package metadata for each workspace runtime package

## Acceptance

- Starting or importing the packaged service entrypoint fails during validation if any runtime module cannot resolve.
- A shared package may add a direct runtime dependency without requiring each service consumer to add that dependency manually.
