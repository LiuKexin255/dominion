import * as fs from "node:fs";
import * as path from "node:path";
import { describe, expect, it } from "vitest";
import { loadProtoAt, PROTO_PATH } from "./server.js";

/**
 * Regression tests for the v1 team proto loading (B1): game.proto no longer
 * declares TeamService, so the server reads
 * projects/game/agent/game_agent_legacy.proto — and runtime_protos must
 * materialize it (and its game.proto closure) into the deployed tar. An
 * undefined `TeamService` at the registration site is exactly the runtime
 * crash these assertions guard against.
 */
describe("v1 team proto loading", () => {
  it("resolves the legacy proto at its canonical runtime path", () => {
    // Same precedent as the v2 server's PROTO_PATH assertion: the path is
    // service-root relative and mirrors the runtime_protos materialization
    // layout (tools/release/deploy/README.md §runtime_protos).
    expect(PROTO_PATH.endsWith(path.join("projects", "game", "agent", "game_agent_legacy.proto"))).toBe(
      true,
    );
  });

  it("loads the legacy proto and exposes the TeamService registration face", () => {
    // The deployed tar materializes runtime_protos at canonical paths under
    // the service root (PROTO_PATH); the raw-source test runfiles keep the
    // workspace layout instead, so this test loads the same file from its
    // runfiles location with the include roots discovered from the runfiles
    // tree (the google/api protos live in their external repo directory).
    const testProtoPath = path.resolve(import.meta.dirname, "..", "game_agent_legacy.proto");
    expect(fs.existsSync(testProtoPath)).toBe(true);

    const proto = loadProtoAt(testProtoPath, testIncludeDirs(testProtoPath));

    expect(proto.projects.game.TeamService).toBeDefined();
    expect(proto.projects.game.TeamService.service).toBeDefined();
    expect(proto.projects.game.TeamService.service.UpdateTeam).toBeDefined();
  });
});

/** Include roots that resolve the proto imports from the runfiles layout. */
function testIncludeDirs(protoPath: string): string[] {
  const canonicalRoot = path.resolve(path.dirname(protoPath), "..", "..", "..");
  const dirs = [canonicalRoot];
  const runfilesRoot = path.resolve(canonicalRoot, "..");
  for (const entry of fs.readdirSync(runfilesRoot, { withFileTypes: true })) {
    if (!entry.isDirectory()) {
      continue;
    }
    const candidate = path.join(runfilesRoot, entry.name);
    if (fs.existsSync(path.join(candidate, "google", "api", "annotations.proto"))) {
      dirs.push(candidate);
    }
  }
  return dirs;
}
