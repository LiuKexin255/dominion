import { describe, expect, it } from "vitest";

import { parseRedisTarget, resolveTarget } from "./target.js";

describe("parseRedisTarget", () => {
  it("parses app/name", () => {
    const got = parseRedisTarget("myapp/myredis");

    expect(got).toEqual({ app: "myapp", service: "myredis" });
  });

  it("trims surrounding whitespace", () => {
    const got = parseRedisTarget(" myapp / myredis ");

    expect(got).toEqual({ app: "myapp", service: "myredis" });
  });

  it.each([
    ["missing slash", "myapp"],
    ["empty app", "/myredis"],
    ["empty service", "myapp/"],
    ["nested service path", "myapp/a/b"],
    ["explicit port", "myapp/myredis:6379"],
    ["blank", "   "],
  ])("rejects %s with the expected-format message", (_name, raw) => {
    const call = () => parseRedisTarget(raw);

    expect(call).toThrow(Error);
    expect(call).toThrow(/"app\/name"/);
  });
});

describe("resolveTarget", () => {
  it("appends the default redis port for the shared resolver", () => {
    const got = resolveTarget({ app: "myapp", service: "myredis" });

    expect(got).toBe("myapp/myredis:6379");
  });
});
