import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  ConsoleReporter,
  OTelReporter,
  installReporter,
  createOTelReporter,
  getReporter,
} from "./reporter";

// Reset reporter state before each test
beforeEach(() => {
  const temp = installReporter(new ConsoleReporter());
  temp(); // uninstall
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

const emitted: any[] = [];

const mockLogger = {
  emit: vi.fn((record: any) => {
    emitted.push(record);
  }),
  enabled: vi.fn(() => true),
};

vi.mock("@opentelemetry/api-logs", () => {
  return {
    logs: {
      getLogger: vi.fn(() => mockLogger),
    },
    SeverityNumber: {
      UNSPECIFIED: 0,
      TRACE: 1,
      TRACE2: 2,
      TRACE3: 3,
      TRACE4: 4,
      DEBUG: 5,
      DEBUG2: 6,
      DEBUG3: 7,
      DEBUG4: 8,
      INFO: 9,
      INFO2: 10,
      INFO3: 11,
      INFO4: 12,
      WARN: 13,
      WARN2: 14,
      WARN3: 15,
      WARN4: 16,
      ERROR: 17,
      ERROR2: 18,
      ERROR3: 19,
      ERROR4: 20,
      FATAL: 21,
      FATAL2: 22,
      FATAL3: 23,
      FATAL4: 24,
    },
  };
});

// vi.mock is hoisted above all imports by vitest, so the mock is active
// when OTelReporter is imported at the top of this file.
describe("OTelReporter emit (mock provider)", () => {
  beforeEach(() => {
    emitted.length = 0;
    mockLogger.emit.mockClear();
  });

  it("emits body as message string (not JSON)", () => {
    const reporter = new OTelReporter("test-emit");
    reporter.write("info", "hello world", { key: "value" });
    expect(emitted).toHaveLength(1);
    expect(emitted[0].body).toBe("hello world");
  });

  it("emits attributes as separate structured object", () => {
    const reporter = new OTelReporter("test-attrs");
    reporter.write("info", "msg", { foo: "bar", count: 42 });
    expect(emitted[0].attributes).toEqual({ foo: "bar", count: 42 });
  });

  it("maps each severity level to correct SeverityNumber and text", () => {
    const reporter = new OTelReporter("test-severity");
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
    const reporter = new OTelReporter("test-error");
    reporter.write("error", "boom", { error: new Error("something broke") });
    expect(emitted[0].attributes.error).toBe("something broke");
    expect(emitted[0].body).toBe("boom");
  });

  it("emits empty attributes object when attrs is empty", () => {
    const reporter = new OTelReporter("test-empty");
    reporter.write("info", "no attrs", {});
    expect(emitted[0].attributes).toEqual({});
    expect(Object.keys(emitted[0]).sort()).toEqual(
      ["severityNumber", "severityText", "body", "attributes"].sort(),
    );
  });

  it("filters out undefined attribute values", () => {
    const reporter = new OTelReporter("test-undefined");
    reporter.write("info", "partial", { present: "yes", absent: undefined });
    expect(emitted[0].attributes).toEqual({ present: "yes" });
    expect(emitted[0].attributes).not.toHaveProperty("absent");
  });

  it("handles null attribute values", () => {
    const reporter = new OTelReporter("test-null");
    reporter.write("info", "nullable", { val: null });
    expect(emitted[0].attributes).toEqual({ val: null });
  });

  it("handles boolean attribute values", () => {
    const reporter = new OTelReporter("test-bool");
    reporter.write("info", "flags", { active: true, deleted: false });
    expect(emitted[0].attributes).toEqual({ active: true, deleted: false });
  });

  it("handles number attribute values", () => {
    const reporter = new OTelReporter("test-num");
    reporter.write("info", "counts", { count: 0, ratio: 3.14 });
    expect(emitted[0].attributes).toEqual({ count: 0, ratio: 3.14 });
  });

  it("body is a plain string, never JSON-encoded", () => {
    const reporter = new OTelReporter("test-body-type");
    reporter.write("warn", "simple message", { data: { nested: true } });
    expect(typeof emitted[0].body).toBe("string");
    expect(emitted[0].body).toBe("simple message");
    expect(emitted[0].attributes.data).toEqual({ nested: true });
  });

  it("absent optional fields produce no sentinel keys", () => {
    const reporter = new OTelReporter("test-no-sentinel");
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
