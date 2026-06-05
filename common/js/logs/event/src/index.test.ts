import { describe, it, expect } from "vitest";
import {
  eventString,
  eventInt,
  eventBool,
  eventErr,
  eventAny,
} from "./index";

// ---------------------------------------------------------------------------
// eventString
// ---------------------------------------------------------------------------
describe("eventString", () => {
  it("creates an Event with a string value", () => {
    const result = eventString("key", "value");
    expect(result).toEqual({ key: "key", value: "value" });
    expect(typeof result.key).toBe("string");
    expect(typeof result.value).toBe("string");
  });
});

// ---------------------------------------------------------------------------
// eventInt
// ---------------------------------------------------------------------------
describe("eventInt", () => {
  it("creates an Event with a positive integer", () => {
    const result = eventInt("count", 42);
    expect(result).toEqual({ key: "count", value: 42 });
    expect(typeof result.key).toBe("string");
    expect(typeof result.value).toBe("number");
  });

  it("creates an Event with a negative integer", () => {
    const result = eventInt("negative", -1);
    expect(result).toEqual({ key: "negative", value: -1 });
    expect(typeof result.value).toBe("number");
  });

  it("creates an Event with zero", () => {
    const result = eventInt("zero", 0);
    expect(result).toEqual({ key: "zero", value: 0 });
    expect(typeof result.value).toBe("number");
  });
});

// ---------------------------------------------------------------------------
// eventBool
// ---------------------------------------------------------------------------
describe("eventBool", () => {
  it("creates an Event with true", () => {
    const result = eventBool("active", true);
    expect(result).toEqual({ key: "active", value: true });
    expect(typeof result.key).toBe("string");
    expect(typeof result.value).toBe("boolean");
  });

  it("creates an Event with false", () => {
    const result = eventBool("inactive", false);
    expect(result).toEqual({ key: "inactive", value: false });
    expect(typeof result.value).toBe("boolean");
  });
});

// ---------------------------------------------------------------------------
// eventErr
// ---------------------------------------------------------------------------
describe("eventErr", () => {
  it("creates an Event with an Error value", () => {
    const error = new Error("test");
    const result = eventErr(error);
    expect(result).toEqual({ key: "error", value: error });
    expect(result.value instanceof Error).toBe(true);
    expect((result.value as Error).message).toBe("test");
  });

  it("returns zero-value Event for null", () => {
    const result = eventErr(null);
    expect(result).toEqual({ key: "", value: undefined });
  });

  it("returns zero-value Event for undefined", () => {
    const result = eventErr(undefined);
    expect(result).toEqual({ key: "", value: undefined });
  });
});

// ---------------------------------------------------------------------------
// eventAny
// ---------------------------------------------------------------------------
describe("eventAny", () => {
  it("creates an Event with an object value", () => {
    const data = { complex: true };
    const result = eventAny("data", data);
    expect(result).toEqual({ key: "data", value: { complex: true } });
    // same object reference preserved
    expect(result.value).toBe(data);
  });

  it("creates an Event with null value", () => {
    const result = eventAny("nullable", null);
    expect(result).toEqual({ key: "nullable", value: null });
  });

  it("creates an Event with a string value", () => {
    const result = eventAny("name", "hello");
    expect(result).toEqual({ key: "name", value: "hello" });
  });

  it("creates an Event with a number value", () => {
    const result = eventAny("age", 25);
    expect(result).toEqual({ key: "age", value: 25 });
  });

  it("creates an Event with a boolean value", () => {
    const result = eventAny("flag", true);
    expect(result).toEqual({ key: "flag", value: true });
  });

  it("creates an Event with an Error value", () => {
    const err = new Error("any error");
    const result = eventAny("err", err);
    expect(result).toEqual({ key: "err", value: err });
  });

  it("creates an Event with undefined value", () => {
    const result = eventAny("undef", undefined);
    expect(result).toEqual({ key: "undef", value: undefined });
  });
});
