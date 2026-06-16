import { describe, it, expect, expectTypeOf } from "vitest";
import type { ResolverState } from "./grpc-types";
import type { Endpoint } from "@grpc/grpc-js/build/src/subchannel-address";

describe("ResolverState discriminated union", () => {
  it("narrows to unresolved on status === 'unresolved'", () => {
    const state: ResolverState = { status: "unresolved" };

    if (state.status === "unresolved") {
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
        const _exhaustive: never = state;
        (_exhaustive as unknown);
      }
    }
  });
});
