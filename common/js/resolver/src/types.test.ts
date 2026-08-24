import { describe, it, expect, expectTypeOf } from "vitest";
import type { PortSelector, StatefulInstance } from "./types.js";
import type { Target } from "./target.js";

// ---------------------------------------------------------------------------
// Type-level tests — these verify that the exported types are structurally
// correct at compile time AND exercise the types at runtime.
// ---------------------------------------------------------------------------

describe("PortSelector", () => {
  it("holds a numeric port", () => {
    const selector: PortSelector = { kind: "number", port: 50051 };
    expect(selector.kind).toBe("number");
    expect(selector.port).toBe(50051);
  });
});

// ---------------------------------------------------------------------------
// Sanity checks — verify that the exported types are structurally correct.
// ---------------------------------------------------------------------------

describe("exported types", () => {
  it("StatefulInstance has the expected shape", () => {
    const inst: StatefulInstance = {
      index: 0,
      endpoints: ["10.0.0.1:50051"],
    };
    expect(inst.index).toBe(0);
    expect(inst.endpoints).toEqual(["10.0.0.1:50051"]);

    // hostname is optional
    const withHostname: StatefulInstance = {
      index: 1,
      endpoints: ["10.0.0.2:50051"],
      hostname: "pod-1",
    };
    expect(withHostname.hostname).toBe("pod-1");
  });

  it("Target interface is re-exported", () => {
    // This is a type-level assertion — it will fail to compile if Target
    // is not structurally compatible.
    const _target: Target = {
      app: "my-app",
      service: "my-service",
      port: { kind: "number", port: 50051 },
    };
    expectTypeOf(_target.app).toEqualTypeOf<string>();
    expectTypeOf(_target.service).toEqualTypeOf<string>();
    expect(_target.port).toEqual({ kind: "number", port: 50051 });
  });
});
