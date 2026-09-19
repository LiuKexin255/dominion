import { createHmac } from "node:crypto";

const MONGO_PASSWORD_HMAC_KEY = "dominion-mongo-stable-password";
const PASSWORD_ALPHABET =
  "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
const PASSWORD_MIN_LEN = 24;
const PASSWORD_JOINER = "\x00";

/**
 * Derives the deterministic mongo admin password — byte-for-byte the Go
 * algorithm (`common/gopkg/mongo/credentials.go`, `dominion/common/gopkg/mongo`):
 * HMAC-SHA256 over the trim-normalized inputs joined with NUL, each digest
 * byte mapped onto the alphanumeric alphabet; a digest shorter than the
 * minimum length is extended by re-walking the digest bytes.
 *
 * Module-internal: not re-exported from the package entry point.
 */
export function deriveStablePassword(inputs: readonly string[]): string {
  const normalized = inputs.map((input) => input.trim());
  const mac = createHmac("sha256", MONGO_PASSWORD_HMAC_KEY);
  mac.update(normalized.join(PASSWORD_JOINER));
  const sum = mac.digest();

  const encoded: string[] = [];
  for (const byte of sum) {
    encoded.push(PASSWORD_ALPHABET[byte % PASSWORD_ALPHABET.length]);
  }
  while (encoded.length < PASSWORD_MIN_LEN) {
    for (const byte of sum) {
      if (encoded.length >= PASSWORD_MIN_LEN) {
        break;
      }
      encoded.push(PASSWORD_ALPHABET[byte % PASSWORD_ALPHABET.length]);
    }
  }

  return encoded.join("");
}
