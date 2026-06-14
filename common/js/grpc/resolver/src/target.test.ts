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

  describe("dominion:/// scheme prefix", () => {
    it("strips the scheme and parses correctly", () => {
      const result = parseTarget("dominion:///myapp/myservice:50051");
      expect(result).toEqual<Target>({
        app: "myapp",
        service: "myservice",
        port: { kind: "number", port: 50051 },
      });
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

  describe("reject non-numeric port", () => {
    it("throws InvalidTargetError for named port (grpc)", () => {
      expect(() => parseTarget("app/service:grpc")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError for named port with hyphens", () => {
      expect(() => parseTarget("app/service:my-port")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError for mixed alphanumeric port", () => {
      expect(() => parseTarget("app/service:0abc")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError for port with underscore", () => {
      expect(() => parseTarget("app/service:my_port")).toThrow(InvalidTargetError);
    });

    it("throws InvalidTargetError for port with uppercase letter", () => {
      expect(() => parseTarget("app/service:Grpc")).toThrow(InvalidTargetError);
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

    it("throws InvalidTargetError when service contains slash", () => {
      expect(() => parseTarget("app/sub/service:50051")).toThrow(
        InvalidTargetError,
      );
    });

    it("throws InvalidTargetError for port with leading sign", () => {
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
