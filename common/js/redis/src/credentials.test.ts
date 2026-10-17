import { describe, expect, it } from "vitest";

import { deriveStablePassword } from "./credentials.js";

describe("deriveStablePassword", () => {
  it("matches the instance-side cross-implementation vector (myapp/dev/myredis)", () => {
    const got = deriveStablePassword(["myapp", "dev", "myredis"]);

    expect(got).toBe("k1OTVRTOdkt9XeuhOVEk5yR82ZZqnjxt");
  });

  it("trims each input before joining", () => {
    const got = deriveStablePassword([" myapp ", " dev ", " myredis "]);

    expect(got).toBe("k1OTVRTOdkt9XeuhOVEk5yR82ZZqnjxt");
  });

  it("derives the default-environment password for missing environment", () => {
    const got = deriveStablePassword(["myapp", "default", "myredis"]);

    expect(got).toBe("FF53SVZiUNQCFp1k4B2c0fp4P4NCM729");
  });

  it("produces a password of at least the minimum length", () => {
    const got = deriveStablePassword(["", "", ""]);

    expect(got).toBe("9fNf1YzJzh715ytcap6lALAFz3EbMGrD");
  });
});
