import { describe, it, expect, vi, afterEach } from "vitest";
import { Logger, defaultLogger, LogLevel, type LogAttributes } from "./logger";
import { installReporter, ConsoleReporter } from "./reporter";

// Inline event constructors — @dominion/common-js-logs-event is not
// resolvable inside Bazel's vitest sandbox so we recreate the tiny
// Event shape here.
type TestEvent = { key: string; value: string | number | Error | undefined };

function eventString(key: string, value: string): TestEvent {
  return { key, value };
}

function eventInt(key: string, value: number): TestEvent {
  return { key, value };
}

function eventErr(err: Error | null | undefined): TestEvent {
  if (!err) return { key: "", value: undefined };
  return { key: "error", value: err };
}

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
      const logger = new Logger(LogLevel.DEBUG);
      logger.info("hello", { user: "alice" });
      restore();

      const parsed = parseLine(output);
      expect(parsed.level).toBe("INFO");
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
      expect(parsed.level).toBe("INFO");
      expect(parsed.msg).toBe("reported");
      expect(parsed.key).toBe("val");
    });
  });

  // -------------------------------------------------------------------------
  // debug
  // -------------------------------------------------------------------------
  describe("debug", () => {
    it("produces output when log level is DEBUG", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.DEBUG);
      logger.debug("debug msg");
      restore();

      expect(output.length).toBe(1);
      expect(JSON.parse(output[0]).level).toBe("DEBUG");
    });

    it("is suppressed when log level is INFO (default)", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.INFO);
      logger.debug("should not appear");
      restore();

      expect(output.length).toBe(0);
    });

    it("is suppressed when log level is WARN", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.WARN);
      logger.debug("should not appear");
      restore();

      expect(output.length).toBe(0);
    });

    it("is suppressed when log level is ERROR", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.ERROR);
      logger.debug("should not appear");
      restore();

      expect(output.length).toBe(0);
    });
  });

  // -------------------------------------------------------------------------
  // warn and error
  // -------------------------------------------------------------------------
  describe("warn and error", () => {
    it("warn produces output at INFO level", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.INFO);
      logger.warn("warning");
      restore();

      expect(output.length).toBe(1);
      expect(JSON.parse(output[0]).level).toBe("WARN");
    });

    it("error produces output at INFO level", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.INFO);
      logger.error("error");
      restore();

      expect(output.length).toBe(1);
      expect(JSON.parse(output[0]).level).toBe("ERROR");
    });

    it("warn produces output at WARN level", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.WARN);
      logger.warn("warning");
      restore();

      expect(output.length).toBe(1);
    });

    it("error produces output at ERROR level", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.ERROR);
      logger.error("error");
      restore();

      expect(output.length).toBe(1);
    });
  });

  // -------------------------------------------------------------------------
  // events merging
  // -------------------------------------------------------------------------
  describe("events", () => {
    it("merges event fields into attributes", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.DEBUG);
      logger.info(
        "event test",
        { base: "attr" },
        eventString("extra", "value"),
        eventInt("count", 42),
      );
      restore();

      const parsed = parseLine(output);
      expect(parsed.base).toBe("attr");
      expect(parsed.extra).toBe("value");
      expect(parsed.count).toBe(42);
    });

    it("skips zero-value events", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.DEBUG);
      logger.info("skip test", { base: "attr" }, eventErr(null));
      restore();

      const parsed = parseLine(output);
      expect(parsed.base).toBe("attr");
      // eventErr(null) produces { key: "", value: undefined } — silently skipped
      expect(parsed[""]).toBeUndefined();
    });

    it("includes eventErr when given a real Error", () => {
      const { output, restore } = captureConsole();
      const logger = new Logger(LogLevel.DEBUG);
      const err = new Error("boom");
      logger.info("with error", {}, eventErr(err));
      restore();

      const parsed = parseLine(output);
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
