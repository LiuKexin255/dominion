/**
 * Recursive deep merge implementation for the `@dominion/common-js-config` SDK.
 *
 * Semantics follow `specs/045-deploy-config/data-model.md` "Deep Merge
 * Semantics": plain objects merge recursively (keys present in the source
 * override the target, keys absent in the source keep the target value);
 * arrays and scalars are replaced wholesale by the source value (arrays are
 * NOT merged by index); `undefined` source values never override; `null`
 * source values do override. Prototype-polluting keys (`__proto__`,
 * `constructor`, `prototype`) are skipped.
 */

const POLLUTING_KEYS = ["__proto__", "constructor", "prototype"];

function isPlainObject(value: unknown): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    return false;
  }
  const proto = Object.getPrototypeOf(value);
  return proto === Object.prototype || proto === null;
}

/**
 * Recursively merge `source` into `target` in place and return `target`.
 *
 * `target` MUST be a deep clone owned by the caller (e.g. produced by
 * `structuredClone`); the function never mutates `source`. Values taken from
 * `source` are `structuredClone`d on assignment so the result shares no
 * references with `source`.
 */
export function deepMerge<T extends object>(
  target: T,
  source: Record<string, unknown>,
): T {
  for (const key of Object.keys(source)) {
    if (POLLUTING_KEYS.includes(key)) {
      continue;
    }
    const srcValue = source[key];
    if (srcValue === undefined) {
      continue;
    }
    const tgtValue = (target as Record<string, unknown>)[key];
    if (isPlainObject(srcValue) && isPlainObject(tgtValue)) {
      (target as Record<string, unknown>)[key] = deepMerge(tgtValue, srcValue);
    } else {
      (target as Record<string, unknown>)[key] = structuredClone(srcValue);
    }
  }
  return target;
}
