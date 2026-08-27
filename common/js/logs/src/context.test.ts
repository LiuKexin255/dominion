import { describe, it, expect } from "vitest";
import { currentLogger, currentAttributes, withAttributes, withLogger } from "./context.js";
import { Logger, defaultLogger, LogLevel, type LogAttributes } from "./logger.js";

describe("currentLogger", () => {
  it("returns default logger when no scope active", () => {
    const logger = currentLogger();
    expect(logger).toBe(defaultLogger());
  });
});

describe("currentAttributes", () => {
  it("returns empty object when no scope active", () => {
    expect(currentAttributes()).toEqual({});
  });
});

describe("withAttributes", () => {
  it("provides attributes in scope", () => {
    let captured: LogAttributes = {};
    withAttributes({ reqId: "123" }, () => {
      captured = currentAttributes();
    });
    expect(captured).toEqual({ reqId: "123" });
  });

  it("merges attributes from nested scope", () => {
    let captured: LogAttributes = {};
    withAttributes({ outer: "outer" }, () => {
      withAttributes({ inner: "inner" }, () => {
        captured = currentAttributes();
      });
    });
    expect(captured).toEqual({ outer: "outer", inner: "inner" });
  });

  it("inner attributes override outer on key collision", () => {
    let captured: LogAttributes = {};
    withAttributes({ key: "outer" }, () => {
      withAttributes({ key: "inner" }, () => {
        captured = currentAttributes();
      });
    });
    expect(captured).toEqual({ key: "inner" });
  });

  it("returns the callback return value", () => {
    const result = withAttributes({ a: 1 }, () => 42);
    expect(result).toBe(42);
  });

  it("scope is cleaned up after callback", () => {
    withAttributes({ temp: true }, () => {
      // active
    });
    expect(currentAttributes()).toEqual({});
  });
});

describe("withLogger", () => {
  it("switches logger within scope", () => {
    const custom = new Logger("debug");
    let captured: Logger | undefined;
    withLogger(custom, () => {
      captured = currentLogger();
    });
    expect(captured).toBe(custom);
  });

  it("withLogger(null) uses current logger (falls back to default)", () => {
    let captured: Logger | undefined;
    withLogger(null, () => {
      captured = currentLogger();
    });
    expect(captured).toBe(defaultLogger());
  });

  it("withLogger(undefined) uses current logger (falls back to default)", () => {
    let captured: Logger | undefined;
    withLogger(undefined, () => {
      captured = currentLogger();
    });
    expect(captured).toBe(defaultLogger());
  });

  it("default logger outside scope after withLogger returns", () => {
    const custom = new Logger("debug");
    withLogger(custom, () => {
      // scope active
    });
    expect(currentLogger()).toBe(defaultLogger());
  });

  it("returns the callback return value", () => {
    const custom = new Logger("debug");
    const result = withLogger(custom, () => "hello");
    expect(result).toBe("hello");
  });

  it("preserves parent attributes when switching logger", () => {
    const custom = new Logger("debug");
    let capturedAttrs: LogAttributes = {};
    let capturedLogger: Logger | undefined;
    withAttributes({ zone: "a" }, () => {
      withLogger(custom, () => {
        capturedAttrs = currentAttributes();
        capturedLogger = currentLogger();
      });
    });
    expect(capturedAttrs).toEqual({ zone: "a" });
    expect(capturedLogger).toBe(custom);
  });
});
