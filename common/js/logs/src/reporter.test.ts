import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  ConsoleReporter,
  OTelReporter,
  installReporter,
  newOTelReporter,
  getReporter,
  LogLevel,
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
    reporter.write(LogLevel.INFO, "test", { key: "value" });
    expect(writeSpy).toHaveBeenCalledOnce();
    const output = JSON.parse(writeSpy.mock.calls[0][0] as string);
    expect(output.level).toBe("INFO");
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
  it("creates reporter via newOTelReporter", () => {
    const reporter = newOTelReporter("test");
    expect(reporter).toBeInstanceOf(OTelReporter);
  });

  it("write is no-op when no LoggerProvider configured", () => {
    const reporter = newOTelReporter("test-no-provider");
    // Should not throw when no global LoggerProvider
    expect(() => reporter.write(LogLevel.INFO, "msg", {})).not.toThrow();
  });
});
