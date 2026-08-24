import { describe, it, expect, vi, afterEach } from "vitest";
import { Logger, defaultLogger, type LogAttributes } from "./logger.js";
import { installReporter, ConsoleReporter } from "./reporter.js";

// LogLevel is now a string union: "debug" | "info" | "warn" | "error"
// Tests use string literals directly instead of enum members.

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Captures console.log calls and returns the recorded string arguments. */
function captureConsole(): { output: string[]; restore: () => void } {
  const output: string[] = [];
  const spy = vi.spyOn(console, "log").mockImplementation((msg: string) => {
    output.push(msg);
  });
  return { output, restore: () => spy.mockRestore() };
}

/** Captures process.stdout.write calls (used by ConsoleReporter). */
function captureStdout(): { output: string[]; restore: () => void } {
  const output: string[] = [];
  const spy = vi
    .spyOn(process.stdout, "write")
    .mockImplementation((chunk: unknown) => {
      output.push(String(chunk).trim());
      return true;
    });
  return { output, restore: () => spy.mockRestore() };
}

/** Parses the first output line as JSON. */
function parseLine(output: string[]): Record<string, unknown> {
  expect(output.length).toBeGreaterThanOrEqual(1);
  return JSON.parse(output[0]);
}

// ---------------------------------------------------------------------------
// Logger tests
// ---------------------------------------------------------------------------

describe("Logger", () => {
  let restoreReporter: (() => void) | null = null;

  afterEach(() => {
    if (restoreReporter) {
      restoreReporter();
      restoreReporter = null;
    }
  });

  // -------------------------------------------------------------------------
  // info
  // -------------------------------------------------------------------------
  describe("info", () => {
    it("writes structured JSON to console.log when no reporter installed", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("debug");
      logger.info("hello", { user: "alice" });
      restore();

      const parsed = parseLine(output);
      expect(parsed.level).toBe("info");
      expect(parsed.msg).toBe("hello");
      expect(parsed.user).toBe("alice");
    });

    it("routes through ConsoleReporter when installed", () => {
      restoreReporter = installReporter(new ConsoleReporter());
      const { output, restore } = captureStdout();
      const logger = new Logger();
      logger.info("reported", { key: "val" });
      restore();

      const parsed = parseLine(output);
      expect(parsed.level).toBe("info");
      expect(parsed.msg).toBe("reported");
      expect(parsed.key).toBe("val");
    });
  });

  // -------------------------------------------------------------------------
  // debug
  // -------------------------------------------------------------------------
  describe("debug", () => {
    it("produces output when log level is debug", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("debug");
      logger.debug("debug msg");
      restore();

      expect(output.length).toBe(1);
      expect(JSON.parse(output[0]).level).toBe("debug");
    });

    it("is suppressed when log level is info (default)", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("info");
      logger.debug("should not appear");
      restore();

      expect(output.length).toBe(0);
    });

    it("is suppressed when log level is warn", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("warn");
      logger.debug("should not appear");
      restore();

      expect(output.length).toBe(0);
    });

    it("is suppressed when log level is error", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("error");
      logger.debug("should not appear");
      restore();

      expect(output.length).toBe(0);
    });
  });

  // -------------------------------------------------------------------------
  // warn and error
  // -------------------------------------------------------------------------
  describe("warn and error", () => {
    it("warn produces output at info level", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("info");
      logger.warn("warning");
      restore();

      expect(output.length).toBe(1);
      expect(JSON.parse(output[0]).level).toBe("warn");
    });

    it("error produces output at info level", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("info");
      logger.error("error");
      restore();

      expect(output.length).toBe(1);
      expect(JSON.parse(output[0]).level).toBe("error");
    });

    it("warn produces output at warn level", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("warn");
      logger.warn("warning");
      restore();

      expect(output.length).toBe(1);
    });

    it("error produces output at error level", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("error");
      logger.error("error");
      restore();

      expect(output.length).toBe(1);
    });
  });

  // -------------------------------------------------------------------------
  // attributes
  // -------------------------------------------------------------------------
  describe("attributes", () => {
    it("spreads attribute fields into output", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("debug");
      logger.info("attr test", { base: "attr", extra: "value", count: 42 });
      restore();

      const parsed = parseLine(output);
      expect(parsed.base).toBe("attr");
      expect(parsed.extra).toBe("value");
      expect(parsed.count).toBe(42);
    });

    it("omits undefined attribute values from output", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("debug");
      logger.info("skip test", { base: "attr", empty: undefined });
      restore();

      const parsed = parseLine(output);
      expect(parsed.base).toBe("attr");
      // JSON.stringify drops undefined values
      expect(parsed.empty).toBeUndefined();
    });

    it("includes Error attribute values in output", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger("debug");
      const err = new Error("boom");
      logger.info("with error", { error: err });
      restore();

      const parsed = parseLine(output);
      // Error objects are serialized as {} by JSON.stringify
      expect(parsed.error).toBeDefined();
    });
  });
});

// ---------------------------------------------------------------------------
// defaultLogger singleton
// ---------------------------------------------------------------------------
describe("defaultLogger", () => {
  it("returns the same instance on repeated calls", () => {
    const a = defaultLogger();
    const b = defaultLogger();
    expect(a).toBe(b);
  });
});
