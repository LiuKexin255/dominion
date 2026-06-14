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
