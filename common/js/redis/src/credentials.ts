import { createHmac } from "node:crypto";

const REDIS_PASSWORD_HMAC_KEY = "dominion-redis-stable-password";
const PASSWORD_ALPHABET =
  "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789";
const PASSWORD_MIN_LEN = 24;
const PASSWORD_JOINER = "\x00";

/**
 * Derives the deterministic redis instance password — byte-for-byte the Go
 * algorithm (`common/gopkg/redis/credentials.go`, `dominion/common/gopkg/redis`):
 * HMAC-SHA256 over the trim-normalized inputs joined with NUL, each digest
 * byte mapped onto the alphanumeric alphabet; a digest shorter than the
 * minimum length is extended by re-walking the digest bytes.
 */
export function deriveStablePassword(inputs: readonly string[]): string {
  const normalized = inputs.map((input) => input.trim());
  const mac = createHmac("sha256", REDIS_PASSWORD_HMAC_KEY);
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
