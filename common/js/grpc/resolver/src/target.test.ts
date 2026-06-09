import { describe, expect, it } from "vitest";
import { InvalidTargetError } from "./errors";
import { parseTarget, type Target } from "./target";

describe("parseTarget", () => {
  describe("valid numeric port", () => {
    it("parses app/service:port into a numeric port target", () => {
      const result = parseTarget("myapp/myservice:50051");
      expect(result).toEqual<Target>({
        app: "myapp",
        service: "myservice",
        port: { kind: "number", port: 50051 },
      });
    });

    it("accepts port 0 (minimum valid port)", () => {
      const result = parseTarget("a/b:0");
      expect(result.port).toEqual({ kind: "number", port: 0 });
    });

    it("accepts port 65535 (maximum valid port)", () => {
      const result = parseTarget("a/b:65535");
      expect(result.port).toEqual({ kind: "number", port: 65535 });
    });

    it("accepts leading zeros in numeric port (e.g. 050)", () => {
      const result = parseTarget("a/b:050");
      expect(result.port).toEqual({ kind: "number", port: 50 });
    });
  });

  describe("valid named port", () => {
    it("parses app/service:name into a named port target", () => {
      const result = parseTarget("myapp/myservice:grpc");
      expect(result).toEqual<Target>({
        app: "myapp",
        service: "myservice",
        port: { kind: "name", name: "grpc" },
      });
    });

    it("accepts DNS-label port with hyphens", () => {
      const result = parseTarget("a/b:my-port");
      expect(result.port).toEqual({ kind: "name", name: "my-port" });
    });

    it("accepts DNS-label port with digits", () => {
      const result = parseTarget("a/b:port2");
      expect(result.port).toEqual({ kind: "name", name: "port2" });
    });

    it("accepts single-letter DNS-label port", () => {
      const result = parseTarget("a/b:x");
      expect(result.port).toEqual({ kind: "name", name: "x" });
    });
  });

  describe("dominion:/// scheme prefix", () => {
    it("strips the scheme and parses correctly", () => {
      const result = parseTarget("dominion:///myapp/myservice:50051");
      expect(result).toEqual<Target>({
        app: "myapp",
        service: "myservice",
        port: { kind: "number", port: 50051 },
      });
    });

    it("strips the scheme with named port", () => {
      const result = parseTarget("dominion:///myapp/myservice:grpc");
      expect(result.port).toEqual({ kind: "name", name: "grpc" });
    });
  });

  describe("whitespace trimming", () => {
    it("trims spaces around segments", () => {
      const result = parseTarget("  myapp / myservice : 50051 ");
      expect(result).toEqual<Target>({
        app: "myapp",
        service: "myservice",
        port: { kind: "number", port: 50051 },
      });
    });

    it("trims spaces with scheme prefix", () => {
      const result = parseTarget("  dominion:///  myapp / myservice : 50051  ");
      expect(result).toEqual<Target>({
        app: "myapp",
        service: "myservice",
        port: { kind: "number", port: 50051 },
      });
    });
  });

  describe("reject invalid input", () => {
    it("throws InvalidTargetError for empty string", () => {
      expect(() => parseTarget("")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError for whitespace-only string", () => {
      expect(() => parseTarget("   ")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError when app is missing", () => {
      expect(() => parseTarget("/service:50051")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError when service is missing (empty)", () => {
      expect(() => parseTarget("app/:50051")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError when port is missing (no colon)", () => {
      expect(() => parseTarget("app/service")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError when port is empty (colon but nothing after)", () => {
      expect(() => parseTarget("app/service:")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError for unsupported scheme (http:///)", () => {
      expect(() => parseTarget("http:///app/service:50051")).toThrow(
        InvalidTargetError,
      );
    });

    it("throws InvalidTargetError for unsupported scheme (dns:///)", () => {
      expect(() => parseTarget("dns:///app/service:50051")).toThrow(
        InvalidTargetError,
      );
    });

    it("throws InvalidTargetError for out-of-range port -1", () => {
      expect(() => parseTarget("app/service:-1")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError for out-of-range port 65536", () => {
      expect(() => parseTarget("app/service:65536")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError for named port starting with digit", () => {
      expect(() => parseTarget("app/service:0abc")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError for named port with underscore", () => {
      expect(() => parseTarget("app/service:my_port")).toThrow(
        InvalidTargetError,
      );
    });

    it("throws InvalidTargetError for named port with uppercase letter", () => {
      expect(() => parseTarget("app/service:Grpc")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError when service contains slash", () => {
      expect(() => parseTarget("app/sub/service:50051")).toThrow(
        InvalidTargetError,
      );
    });

    it("throws InvalidTargetError for named port with only digits but leading sign", () => {
      expect(() => parseTarget("app/service:+1")).toThrow(InvalidTargetError);
    });
  });

  describe("error instance type", () => {
    it("parseTarget throws InvalidTargetError (instanceof check)", () => {
      try {
        parseTarget("");
        expect.unreachable("should have thrown");
      } catch (e) {
        expect(e).toBeInstanceOf(InvalidTargetError);
        expect(e).toBeInstanceOf(Error);
        expect((e as InvalidTargetError).name).toBe("InvalidTargetError");
      }
    });
  });
});
