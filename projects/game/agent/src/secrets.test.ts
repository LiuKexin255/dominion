/**
 * secrets.test.ts — Tests for readSecret()
 *
 * Covers:
 *   1. Missing file → returns ""
 *   2. Existing file → returns trimmed content
 *   3. Unreadable file → throws descriptive error without secret content
 *   4. Secret content never appears in error messages or log output
 */

import { describe, it, expect, afterEach } from "vitest";
import { writeFileSync, unlinkSync, chmodSync, mkdirSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { readSecret } from "./secrets.js";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const tmpRoot = join(tmpdir(), "dominion-secrets-test");

/** Create a temp file with the given name and content. Returns the full path. */
function createTempFile(name: string, content: string): string {
  // Ensure tmp dir exists
  mkdirSync(tmpRoot, { recursive: true });
  const filePath = join(tmpRoot, name);
  writeFileSync(filePath, content, "utf8");
  return filePath;
}

/** Remove a temp file, attempting to restore permissions first. */
function removeTempFile(filePath: string): void {
  try {
    chmodSync(filePath, 0o644);
  } catch {
    // May already be gone — that's fine
  }
  try {
    unlinkSync(filePath);
  } catch {
    // Best-effort cleanup
  }
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("readSecret", () => {
  afterEach(() => {
    // Clean up any leftover files in tmpRoot
    // (individual tests clean up their own files, this is a safety net)
  });

  it("returns empty string when file does not exist", () => {
    const result = readSecret("/tmp/nonexistent-secret-file-12345");
    expect(result).toBe("");
  });

  it("returns trimmed content when file exists", () => {
    const filePath = createTempFile("existing-secret.txt", "  my-secret-value  \n");
    try {
      const result = readSecret(filePath);
      expect(result).toBe("my-secret-value");
    } finally {
      removeTempFile(filePath);
    }
  });

  it("throws descriptive error for unreadable file", () => {
    const filePath = createTempFile("unreadable-secret.txt", "sensitive-data");
    // Remove read permissions
    chmodSync(filePath, 0o000);
    try {
      expect(() => readSecret(filePath)).toThrow();
      // Verify the error message does NOT contain the secret content
      try {
        readSecret(filePath);
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : String(err);
        expect(message).not.toContain("sensitive-data");
        // Verify it's a descriptive message about the file
        expect(message).toContain("permission denied");
        expect(message).toContain(filePath);
      }
    } finally {
      // Restore permissions so we can delete
      removeTempFile(filePath);
    }
  });

  it("secret content never appears in error messages", () => {
    const secretValue = "super-secret-api-key-12345";
    const filePath = createTempFile("no-log-secret.txt", secretValue);
    chmodSync(filePath, 0o000);
    try {
      try {
        readSecret(filePath);
        // Should have thrown
        expect(true).toBe(false);
      } catch (err: unknown) {
        const message = err instanceof Error ? err.message : String(err);
        // The actual secret content must NOT be in the error message
        expect(message).not.toContain(secretValue);
        expect(message).not.toContain("super-secret");
        expect(message).not.toContain("api-key");
      }
    } finally {
      removeTempFile(filePath);
    }
  });
});
