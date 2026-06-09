import { describe, expect, it } from "vitest";
import { InvalidTargetError } from "./errors";
import { filterEndpoints } from "./endpoint-filter";
import type { PortSelector } from "./target";

describe("filterEndpoints", () => {
  describe("numeric port", () => {
    const endpoints = [
      "10.0.0.1:50051",
      "10.0.0.2:50051",
      "10.0.0.1:8080",
    ];
    const port: PortSelector = { kind: "number", port: 50051 };

    it("returns only endpoints matching the numeric port", () => {
      const result = filterEndpoints(endpoints, port);
      expect(result).toEqual(["10.0.0.1:50051", "10.0.0.2:50051"]);
    });

    it("does not mutate the input array", () => {
      const original = [...endpoints];
      filterEndpoints(endpoints, port);
      expect(endpoints).toEqual(original);
    });
  });

  describe("named port", () => {
    const endpoints = [
      "10.0.0.1:8080",
      "10.0.0.2:9090",
      "10.0.0.3:3000",
    ];

    it("replaces endpoint ports with the resolved named port", () => {
      const port: PortSelector = { kind: "name", name: "grpc" };
      const ports = { grpc: 50051 };

      const result = filterEndpoints(endpoints, port, ports);
      expect(result).toEqual([
        "10.0.0.1:50051",
        "10.0.0.2:50051",
        "10.0.0.3:50051",
      ]);
    });

    it("works when ports map is empty (no named port found)", () => {
      const port: PortSelector = { kind: "name", name: "missing" };
      expect(() => filterEndpoints(endpoints, port, {})).toThrow(
        InvalidTargetError,
      );
    });

    it("does not mutate the input array", () => {
      const port: PortSelector = { kind: "name", name: "grpc" };
      const ports = { grpc: 50051 };
      const original = [...endpoints];
      filterEndpoints(endpoints, port, ports);
      expect(endpoints).toEqual(original);
    });
  });

  describe("named port not found", () => {
    it("throws InvalidTargetError with a descriptive message", () => {
      const endpoints = ["10.0.0.1:8080"];
      const port: PortSelector = { kind: "name", name: "http" };
      const ports = { grpc: 50051 };

      expect(() => filterEndpoints(endpoints, port, ports)).toThrow(
        InvalidTargetError,
      );
      expect(() => filterEndpoints(endpoints, port, ports)).toThrow(
        /named port "http" not found in service endpoints/,
      );
    });

    it("throws when ports parameter is undefined", () => {
      const endpoints = ["10.0.0.1:8080"];
      const port: PortSelector = { kind: "name", name: "http" };

      expect(() => filterEndpoints(endpoints, port)).toThrow(
        InvalidTargetError,
      );
    });
  });

  describe("deduplication", () => {
    it("removes duplicate endpoints", () => {
      const endpoints = [
        "10.0.0.1:50051",
        "10.0.0.2:50051",
        "10.0.0.1:50051",
      ];
      const port: PortSelector = { kind: "number", port: 50051 };

      const result = filterEndpoints(endpoints, port);
      expect(result).toEqual(["10.0.0.1:50051", "10.0.0.2:50051"]);
    });

    it("deduplicates after named port resolution", () => {
      const endpoints = [
        "10.0.0.1:8080",
        "10.0.0.1:9090",
      ];
      const port: PortSelector = { kind: "name", name: "grpc" };
      const ports = { grpc: 50051 };

      const result = filterEndpoints(endpoints, port, ports);
      expect(result).toEqual(["10.0.0.1:50051"]);
    });
  });

  describe("sorting", () => {
    it("returns endpoints sorted lexicographically", () => {
      const endpoints = [
        "10.0.0.3:50051",
        "10.0.0.1:50051",
        "10.0.0.2:50051",
      ];
      const port: PortSelector = { kind: "number", port: 50051 };

      const result = filterEndpoints(endpoints, port);
      expect(result).toEqual([
        "10.0.0.1:50051",
        "10.0.0.2:50051",
        "10.0.0.3:50051",
      ]);
    });
  });

  describe("empty endpoints", () => {
    it("returns an empty array when endpoints is empty", () => {
      const port: PortSelector = { kind: "number", port: 50051 };

      const result = filterEndpoints([], port);
      expect(result).toEqual([]);
    });

    it("returns an empty array with named port on empty endpoints", () => {
      const port: PortSelector = { kind: "name", name: "grpc" };

      const result = filterEndpoints([], port, { grpc: 50051 });
      expect(result).toEqual([]);
    });
  });

  describe("IPv6 address handling", () => {
    it("filters IPv6 endpoints by numeric port", () => {
      const endpoints = [
        "[::1]:50051",
        "[::1]:8080",
        "[2001:db8::1]:50051",
      ];
      const port: PortSelector = { kind: "number", port: 50051 };

      const result = filterEndpoints(endpoints, port);
      expect(result).toEqual(["[2001:db8::1]:50051", "[::1]:50051"]);
    });

    it("replaces port in IPv6 endpoints for named port", () => {
      const endpoints = [
        "[::1]:8080",
        "[2001:db8::1]:9090",
      ];
      const port: PortSelector = { kind: "name", name: "grpc" };
      const ports = { grpc: 50051 };

      const result = filterEndpoints(endpoints, port, ports);
      expect(result).toEqual([
        "[2001:db8::1]:50051",
        "[::1]:50051",
      ]);
    });
  });

  describe("edge cases", () => {
    it("handles single endpoint", () => {
      const endpoints = ["10.0.0.1:50051"];
      const port: PortSelector = { kind: "number", port: 50051 };

      const result = filterEndpoints(endpoints, port);
      expect(result).toEqual(["10.0.0.1:50051"]);
    });

    it("handles no match for numeric port", () => {
      const endpoints = ["10.0.0.1:8080", "10.0.0.2:9090"];
      const port: PortSelector = { kind: "number", port: 50051 };

      const result = filterEndpoints(endpoints, port);
      expect(result).toEqual([]);
    });
  });
});
