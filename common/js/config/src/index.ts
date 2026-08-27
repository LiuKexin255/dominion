/**
 * `@dominion/common-js-config` — deploy config reading SDK.
 *
 * Reads a config entry `(block, key)` from the platform-injected config
 * directory and deep-merges it over caller-provided defaults.
 *
 * Runtime contract (file discovery, YAML-only parsing, error semantics):
 * `specs/045-deploy-config/contracts/runtime-contract.md` §3.
 */

import { readFileSync } from "node:fs";
import path from "node:path";

import { load } from "js-yaml";

import { deepMerge } from "./merge.js";

const CONFIG_DIR_ENV = "DOMINION_CONFIG_DIR";

interface YamlErrorMark {
  line?: unknown;
  column?: unknown;
}

/**
 * Extract a content-free location from a js-yaml parse error.
 *
 * js-yaml's `YAMLException` carries `mark.line`/`mark.column` (0-based) next
 * to `mark.snippet`/`mark.buffer`, which reproduce the offending source line;
 * `err.message` embeds that snippet too. The mark location is checked by
 * shape (not `instanceof YAMLException`) so the extraction survives any module
 * duplication between the package and its test.
 */
function parseErrorPosition(err: unknown): string {
  const mark = (err as { mark?: YamlErrorMark } | null)?.mark;
  if (
    mark !== undefined &&
    typeof mark.line === "number" &&
    typeof mark.column === "number"
  ) {
    return ` (line ${mark.line + 1}, column ${mark.column + 1})`;
  }
  return "";
}

/**
 * Read the config entry `(block, key)`, deep-merge its content over `defaults`,
 * and return the merged result. `defaults` is never modified.
 *
 * The config file is located at `{DOMINION_CONFIG_DIR}/{block}/{key}`; the
 * directory is discovered via the platform-injected `DOMINION_CONFIG_DIR`
 * environment variable (see `specs/045-deploy-config/contracts/runtime-contract.md`).
 * The file is always parsed as YAML, which accepts both `json`- and `yaml`-typed
 * entries (JSON is a subset of YAML; see research R4).
 *
 * Deep-merge semantics (`specs/045-deploy-config/data-model.md` "Deep Merge
 * Semantics"): plain objects merge recursively; arrays and scalars are replaced
 * wholesale by the config value; `null` overrides, `undefined` does not.
 *
 * Throws an `Error` (message contains block, key and path, never the file
 * content) when `DOMINION_CONFIG_DIR` is not set, the file is missing (the
 * block was not selected by deploy.yaml), the file content cannot be parsed,
 * or the parsed content is not a YAML mapping (top-level scalar/array/null).
 *
 * @param block - config block name (first addressing parameter)
 * @param key - config entry name (second addressing parameter)
 * @param defaults - typed defaults used as the merge base
 * @returns a new object: `defaults` deep-merged with the config entry content
 */
export function readConfig<T extends object>(
  block: string,
  key: string,
  defaults: T,
): T {
  const configDir = process.env[CONFIG_DIR_ENV];
  if (configDir === undefined || configDir === "") {
    throw new Error(
      `${CONFIG_DIR_ENV} is not set; cannot read config entry "${block}/${key}" (not a dominion deployment?)`,
    );
  }
  const filePath = path.join(configDir, block, key);

  let content: string;
  try {
    content = readFileSync(filePath, "utf8");
  } catch (err) {
    throw new Error(
      `config entry "${block}/${key}" not found at ${filePath}: ${(err as Error).message}`,
    );
  }

  let cfgObj: unknown;
  try {
    cfgObj = load(content);
  } catch (err) {
    // js-yaml's YAMLException.message embeds a source snippet of the offending
    // line (e.g. " 1 | secret: [unclosed\n-----^"), so the raw message must
    // NOT be forwarded — surface only the location (contracts/sdk-js.md §2:
    // "不泄漏文件内容").
    throw new Error(
      `failed to parse config entry "${block}/${key}" at ${filePath}${parseErrorPosition(err)}`,
    );
  }

  if (
    cfgObj === null ||
    typeof cfgObj !== "object" ||
    Array.isArray(cfgObj)
  ) {
    throw new Error(
      `config entry "${block}/${key}" at ${filePath} must contain a YAML mapping`,
    );
  }

  // structuredClone gives the merge an independent base so `defaults` is never
  // touched; the merged result is a fresh object sharing no references with
  // either input.
  const base = structuredClone(defaults);
  return deepMerge(base, cfgObj as Record<string, unknown>);
}
