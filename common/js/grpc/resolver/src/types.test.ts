import { describe, it, expect, expectTypeOf } from "vitest";
import type { ResolverState, PortSelector, StatefulInstance } from "./types";
import type { Target } from "./target";
import type { Endpoint } from "@grpc/grpc-js/build/src/subchannel-address";

// ---------------------------------------------------------------------------
// Type-level tests — these verify that the discriminated unions narrow
// correctly at compile time AND exercise the narrowing at runtime.
// ---------------------------------------------------------------------------

describe("ResolverState discriminated union", () => {
  it("narrows to unresolved on status === 'unresolved'", () => {
    const state: ResolverState = { status: "unresolved" };

    if (state.status === "unresolved") {
      // TypeScript narrows: only `status` is available
      expectTypeOf(state).toEqualTypeOf<{ status: "unresolved" }>();
      expect(state.status).toBe("unresolved");
    }
  });

  it("narrows to ready on status === 'ready'", () => {
    const state: ResolverState = {
      status: "ready",
      addresses: ["10.0.0.1:50051"],
      endpoints: [],
      lastUpdatedAt: new Date("2025-01-01T00:00:00Z"),
    };

    if (state.status === "ready") {
      // TypeScript narrows: ready-specific fields become accessible
      expectTypeOf(state.addresses).toEqualTypeOf<string[]>();
      expectTypeOf(state.endpoints).toEqualTypeOf<Endpoint[]>();
      expectTypeOf(state.lastUpdatedAt).toEqualTypeOf<Date>();

      expect(state.addresses).toEqual(["10.0.0.1:50051"]);
      expect(state.lastUpdatedAt).toBeInstanceOf(Date);
    }
  });

  it("narrows to closed on status === 'closed'", () => {
    const state: ResolverState = { status: "closed" };

    if (state.status === "closed") {
      expectTypeOf(state).toEqualTypeOf<{ status: "closed" }>();
      expect(state.status).toBe("closed");
    }
  });

  it("exhaustive switch compiles and covers every variant", () => {
    const state: ResolverState = { status: "unresolved" };

    switch (state.status) {
      case "unresolved": {
        expect(state.status).toBe("unresolved");
        break;
      }
      case "ready": {
        expect(state.addresses).toBeDefined();
        expect(state.endpoints).toBeDefined();
        expect(state.lastUpdatedAt).toBeDefined();
        break;
      }
      case "closed": {
        expect(state.status).toBe("closed");
        break;
      }
      default: {
        // Exhaustiveness check — if a new variant is added to the union
        // without a corresponding case, this line will fail to compile.
        const _exhaustive: never = state;
        (_exhaustive as unknown); // suppress unused-variable warning
      }
    }
  });
});

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
