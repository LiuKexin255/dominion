import { describe, expect, it } from "vitest";

import { MONGO_DEFAULT_PORT, parseMongoTarget, resolveTarget } from "./target.js";

describe("parseMongoTarget", () => {
  it("parses app/name with the default mongo port", () => {
    const got = parseMongoTarget("myapp/mycollection");

    expect(got).toEqual({
      app: "myapp",
      service: "mycollection",
      port: MONGO_DEFAULT_PORT,
    });
  });

  it("parses dominion:///app/service:port with the explicit port", () => {
    const got = parseMongoTarget("dominion:///game/mongo:27017");

    expect(got).toEqual({ app: "game", service: "mongo", port: 27017 });
  });

  it("trims surrounding whitespace", () => {
    const got = parseMongoTarget("  dominion:///  myapp / mycollection : 27018  ");

    expect(got).toEqual({ app: "myapp", service: "mycollection", port: 27018 });
  });

  it.each([
    ["missing slash", "myapp"],
    ["empty app", "/mycollection"],
    ["empty service", "myapp/"],
    ["nested service path", "myapp/a/b"],
    ["non-numeric port", "myapp/mycollection:mongoport"],
    ["empty port", "myapp/mycollection:"],
    ["port zero", "myapp/mycollection:0"],
    ["port out of range", "myapp/mycollection:65536"],
    ["unsupported scheme", "http:///myapp/mycollection:27017"],
    ["blank", "   "],
  ])("rejects %s with the expected-format message", (_name, raw) => {
    const call = () => parseMongoTarget(raw);

    expect(call).toThrow(Error);
    expect(call).toThrow(/"app\/name"/);
  });
});

describe("resolveTarget", () => {
  it.each([
    ["app/name with the default port", "myapp/mycollection", "myapp/mycollection:27017"],
    ["dominion scheme with an explicit port", "dominion:///game/mongo:27017", "game/mongo:27017"],
    ["dominion scheme without a port (default applied)", "dominion:///game/mongo", "game/mongo:27017"],
  ])("normalizes %s to the shared resolver form", (_name, raw, want) => {
    const got = resolveTarget(parseMongoTarget(raw));

    expect(got).toBe(want);
  });
});
