/**
 * secrets.ts — Synchronous secret file reader.
 *
 * Reads a single secret value from an optional mounted file.
 * Missing file → empty string (secrets are optional).
 * Unreadable/malformed file → descriptive error WITHOUT secret content.
 */

import { readFileSync, existsSync, constants } from "node:fs";
import { accessSync } from "node:fs";

/**
 * Read a secret from the given file path.
 *
 * @param secretPath - Absolute path to the secret file.
 * @returns The trimmed file contents, or `""` if the file does not exist.
 * @throws If the file exists but cannot be read (permissions, broken symlink, etc.).
 *         The error message describes the problem WITHOUT exposing the secret value.
 */
export function readSecret(secretPath: string): string {
  if (!existsSync(secretPath)) {
    return "";
  }

  // existsSync returned true but we guard against edge cases like
  // broken symlinks or races by catching ENOENT anyway.
  try {
    // Verify readability before reading so we can give a clearer error.
    accessSync(secretPath, constants.R_OK);
  } catch {
    throw new Error(
      `Cannot read secret file: ${secretPath} — permission denied or inaccessible`,
    );
  }

  try {
    const content = readFileSync(secretPath, "utf8");
    return content.trim();
  } catch {
    throw new Error(
      `Cannot read secret file: ${secretPath} — file is not readable or is malformed`,
    );
  }
}
