/**
 * The message of a thrown value, for the model-facing failure texts the tool
 * surface renders after `memory failed: ` / `wiki failed: ` (an unknown throw
 * is stringified). Shared by the memory and wiki operation cores so both
 * report failures identically
 * (specs/068-wiki-round-pause/spec.md FR-011).
 */
export function describeErr(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}
