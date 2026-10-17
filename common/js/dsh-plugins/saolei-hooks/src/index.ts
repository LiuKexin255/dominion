/**
 * cordis plugin entry for the saolei hook engine
 * (`@dominion/dsh-saolei-hooks`, specs/078-saolei-js-hooks/contracts/
 * saolei-hooks-plugin.md §1): the composition's `saoleiHooks` capability
 * provider, mounted as a saolei-loop `config.plugins` row by bare package name
 * and read back by the orchestration to assemble the planner tool face and the
 * player board-change handler over one shared registry
 * (specs/078-saolei-js-hooks/data-model.md §3.1).
 *
 * This module declares the plugin identity the composition manifest references;
 * the engine-wide resource caps shared by the registry and the sandbox live in
 * `./limits.js`.
 */

/** The plugin identifier the composition manifest references. */
export const name = "saolei-hooks";

/** Pure provider: the mount is exposed at plugin load, nothing is required. */
export const inject: string[] = [];

/** The provide name the saolei-loop orchestration reads the mount from. */
export const SAOLEI_HOOKS_SERVICE = "saoleiHooks";
