import { describe, expect, it } from "vitest";

import { deriveStablePassword } from "./credentials.js";

/**
 * Cross-implementation vectors: the deploy instance side pins the same
 * derivation in `projects/infra/deploy/runtime/k8s/builder_test.go`
 * (`Test_generateStablePassword_Vectors`) and the Go library in
 * `common/gopkg/mongo/credentials_test.go`; all three implementations must stay
 * byte-for-byte identical (specs/067-redis-infra-support/research.md D4,
 * data-model.md §4).
 */
describe("deriveStablePassword", () => {
  it("matches the cross-implementation vector (app/env/svc)", () => {
    const got = deriveStablePassword(["app", "env", "svc"]);

    expect(got).toBe("LqDkCZvBx7iJeqJAAl88PshBsDqTJZUc");
  });

  it("matches the Go library vector (app/dev/mongo-main)", () => {
    const got = deriveStablePassword(["app", "dev", "mongo-main"]);

    expect(got).toBe("ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ");
  });

  it("trims each input before joining", () => {
    const got = deriveStablePassword([" app ", " dev ", " mongo-main "]);

    expect(got).toBe("ZOp8SzWTYjjDRAtgSa3MgPeRQ8Zp3aZQ");
  });

  it("matches the Go library vector for empty inputs", () => {
    const got = deriveStablePassword(["", "", ""]);

    expect(got).toBe("FOa0JUWqbmTlXnNU7UpqWknphFv38Dqe");
  });
});
