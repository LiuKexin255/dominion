import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  ConsoleReporter,
  OTelReporter,
  installReporter,
  createOTelReporter,
  getReporter,
  resetReporterForTesting,
} from "./reporter.js";

// Reset reporter singleton to a known-null baseline before each test.
// `installReporter(temp); temp()` only restores the *previous* value, so once
// any test leaks a reporter the idiom propagates the leak; force-clear instead.
beforeEach(() => {
  resetReporterForTesting();
});

describe("ConsoleReporter", () => {
  it("writes JSON to stdout", () => {
    const writeSpy = vi
      .spyOn(process.stdout, "write")
      .mockImplementation(() => true);
    const reporter = new ConsoleReporter();
    reporter.write("info", "test", { key: "value" });
    expect(writeSpy).toHaveBeenCalledOnce();
    const output = JSON.parse(writeSpy.mock.calls[0][0] as string);
    expect(output.level).toBe("info");
    expect(output.msg).toBe("test");
    expect(output.key).toBe("value");
    expect(output.time).toBeDefined();
    writeSpy.mockRestore();
  });
});

describe("installReporter", () => {
  it("throws Error when reporter is null", () => {
    expect(() => installReporter(null)).toThrow(
      "logs: installReporter called with null reporter",
    );
  });

  it("returns an uninstall function", () => {
    const reporter = new ConsoleReporter();
    const uninstall = installReporter(reporter);
    expect(typeof uninstall).toBe("function");
    uninstall();
  });

  it("installed reporter is returned by getReporter", () => {
    const reporter = new ConsoleReporter();
    const uninstall = installReporter(reporter);
    expect(getReporter()).toBe(reporter);
    uninstall();
  });

  it("getReporter returns null after uninstall", () => {
    const reporter = new ConsoleReporter();
    const uninstall = installReporter(reporter);
    uninstall();
    expect(getReporter()).toBeNull();
  });

  it("replacement: installing B after A replaces A", () => {
    const a = new ConsoleReporter();
    const b = new ConsoleReporter();
    installReporter(a);
    const uninstallB = installReporter(b);
    expect(getReporter()).toBe(b);
    uninstallB();
  });

  it("identity-based uninstall: uninstalling A when B is active is no-op", () => {
    const a = new ConsoleReporter();
    const b = new ConsoleReporter();
    const uninstallA = installReporter(a);
    const uninstallB = installReporter(b);
    // Uninstall A should be no-op since B replaced it
    uninstallA();
    expect(getReporter()).toBe(b);
    uninstallB();
  });

  it("double uninstall is safe (no-op)", () => {
    const reporter = new ConsoleReporter();
    const uninstall = installReporter(reporter);
    uninstall();
    uninstall(); // second call should not throw
    expect(getReporter()).toBeNull();
  });
});

describe("OTelReporter", () => {
  it("creates reporter via createOTelReporter", () => {
    const reporter = createOTelReporter("test");
    expect(reporter).toBeInstanceOf(OTelReporter);
  });

  it("write is no-op when no LoggerProvider configured", () => {
    const reporter = createOTelReporter("test-no-provider");
    // Should not throw when no global LoggerProvider
    expect(() => reporter.write("info", "msg", {})).not.toThrow();
  });
});

// Reliable pattern (FR-009): inject the logger via the OTelReporter ctor DI seam
// instead of a module-level vi.mock("@opentelemetry/api-logs"). The module mock is
// fragile under Bazel js_test — the pre-compiled :lib's import of
// @opentelemetry/api-logs bypasses vitest's mock registry (see research.md §2 and
// style/javascript.md §测试). The injected logger is a plain vi.fn(), which works
// identically under the vitest CLI and Bazel.
describe("OTelReporter emit (mock provider)", () => {
  let emitted: any[];
  let mockLogger: { emit: ReturnType<typeof vi.fn>; enabled: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    emitted = [];
    mockLogger = {
      emit: vi.fn((record: any) => {
        emitted.push(record);
      }),
      enabled: vi.fn(() => true),
    };
  });

  it("emits body as message string (not JSON)", () => {
    const reporter = new OTelReporter("test-emit", mockLogger as any);
    reporter.write("info", "hello world", { key: "value" });
    expect(mockLogger.emit).toHaveBeenCalledOnce();
    expect(emitted).toHaveLength(1);
    expect(emitted[0].body).toBe("hello world");
  });

  it("emits attributes as separate structured object", () => {
    const reporter = new OTelReporter("test-attrs", mockLogger as any);
    reporter.write("info", "msg", { foo: "bar", count: 42 });
    expect(emitted[0].attributes).toEqual({ foo: "bar", count: 42 });
  });

  it("maps each severity level to correct SeverityNumber and text", () => {
    const reporter = new OTelReporter("test-severity", mockLogger as any);
    const levels: Array<["debug" | "info" | "warn" | "error", number]> = [
      ["debug", 5],
      ["info", 9],
      ["warn", 13],
      ["error", 17],
    ];
    for (const [level, expectedSeverity] of levels) {
      reporter.write(level, `msg-${level}`, {});
    }
    expect(emitted).toHaveLength(4);
    for (let i = 0; i < levels.length; i++) {
      const [level, expectedSeverity] = levels[i];
      expect(emitted[i].severityNumber).toBe(expectedSeverity);
      expect(emitted[i].severityText).toBe(level);
    }
  });

  it("converts Error values to their .message string", () => {
    const reporter = new OTelReporter("test-error", mockLogger as any);
    reporter.write("error", "boom", { error: new Error("something broke") });
    expect(emitted[0].attributes.error).toBe("something broke");
    expect(emitted[0].body).toBe("boom");
  });

  it("emits empty attributes object when attrs is empty", () => {
    const reporter = new OTelReporter("test-empty", mockLogger as any);
    reporter.write("info", "no attrs", {});
    expect(emitted[0].attributes).toEqual({});
    expect(Object.keys(emitted[0]).sort()).toEqual(
      ["severityNumber", "severityText", "body", "attributes"].sort(),
    );
  });

  it("filters out undefined attribute values", () => {
    const reporter = new OTelReporter("test-undefined", mockLogger as any);
    reporter.write("info", "partial", { present: "yes", absent: undefined });
    expect(emitted[0].attributes).toEqual({ present: "yes" });
    expect(emitted[0].attributes).not.toHaveProperty("absent");
  });

  it("handles null attribute values", () => {
    const reporter = new OTelReporter("test-null", mockLogger as any);
    reporter.write("info", "nullable", { val: null });
    expect(emitted[0].attributes).toEqual({ val: null });
  });

  it("handles boolean attribute values", () => {
    const reporter = new OTelReporter("test-bool", mockLogger as any);
    reporter.write("info", "flags", { active: true, deleted: false });
    expect(emitted[0].attributes).toEqual({ active: true, deleted: false });
  });

  it("handles number attribute values", () => {
    const reporter = new OTelReporter("test-num", mockLogger as any);
    reporter.write("info", "counts", { count: 0, ratio: 3.14 });
    expect(emitted[0].attributes).toEqual({ count: 0, ratio: 3.14 });
  });

  it("body is a plain string, never JSON-encoded", () => {
    const reporter = new OTelReporter("test-body-type", mockLogger as any);
    reporter.write("warn", "simple message", { data: { nested: true } });
    expect(typeof emitted[0].body).toBe("string");
    expect(emitted[0].body).toBe("simple message");
    expect(emitted[0].attributes.data).toEqual({ nested: true });
  });

  it("absent optional fields produce no sentinel keys", () => {
    const reporter = new OTelReporter("test-no-sentinel", mockLogger as any);
    reporter.write("info", "clean", { key: "val" });
    const record = emitted[0];
    const keys = Object.keys(record);
    expect(keys.sort()).toEqual(
      ["severityNumber", "severityText", "body", "attributes"].sort(),
    );
    expect(record).not.toHaveProperty("timestamp");
    expect(record).not.toHaveProperty("eventName");
    expect(record).not.toHaveProperty("context");
    expect(record).not.toHaveProperty("exception");
  });
});
