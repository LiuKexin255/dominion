/**
 * cli.ts — `saolei-recognize` CLI: recognize a minesweeper screenshot and
 * print the board state. Used to validate/tune the recognition profile against
 * real screenshots before freezing golden unit tests.
 *
 * Usage:
 *   saolei-recognize <screenshot.png>           # print text board
 *   saolei-recognize <screenshot.png> --json    # machine-readable GameState
 *   saolei-recognize <screenshot.png> --debug   # per-cell diagnostics
 *   saolei-recognize <screenshot.png> --width 9 --height 9   # override dims
 *
 * Via Bazel: `bazel run //projects/game/pkg/saolei-board:cli -- <path>`.
 */

import { readFileSync } from "node:fs";

import { recognizeBoard } from "../core/recognize";
import { renderBoardText } from "../core/render";

interface ParsedArgs {
  path: string;
  json: boolean;
  debug: boolean;
  width?: number;
  height?: number;
}

function parseArgs(argv: string[]): ParsedArgs | null {
  const rest = argv.slice(2);
  if (rest.length === 0) return null;
  let path = "";
  let json = false;
  let debug = false;
  let width: number | undefined;
  let height: number | undefined;
  for (let i = 0; i < rest.length; i++) {
    const a = rest[i];
    if (a === "--json") json = true;
    else if (a === "--debug") debug = true;
    else if (a === "--width") width = parseInt(rest[++i], 10);
    else if (a === "--height") height = parseInt(rest[++i], 10);
    else if (a === "-h" || a === "--help") return null;
    else if (!a.startsWith("--") && path === "") path = a;
  }
  if (path === "") return null;
  return { path, json, debug, width, height };
}

function usage(): string {
  return [
    "usage: saolei-recognize <screenshot.png> [options]",
    "",
    "options:",
    "  --json            output JSON GameState",
    "  --debug           per-cell diagnostics (sampled color, bevel, winner)",
    "  --width N         override auto-detected column count",
    "  --height N        override auto-detected row count",
    "  -h, --help        show this help",
  ].join("\n");
}

/** CLI entry point. Reads a PNG, recognizes, and prints the board. */
export function main(argv: string[]): number {
  const args = parseArgs(argv);
  if (!args) {
    process.stdout.write(usage() + "\n");
    return args === null && argv.slice(2).length === 0 ? 0 : 1;
  }

  let bytes: Buffer;
  try {
    bytes = readFileSync(args.path);
  } catch (err) {
    process.stderr.write(
      `error: cannot read ${args.path}: ${err instanceof Error ? err.message : String(err)}\n`,
    );
    return 2;
  }

  const result = recognizeBoard(bytes, {
    width: args.width,
    height: args.height,
    collectDiagnostics: args.debug,
  });

  if (args.json) {
    process.stdout.write(JSON.stringify(result.state) + "\n");
    return 0;
  }

  process.stdout.write(renderBoardText(result.state) + "\n");

  if (args.debug && result.diagnostics) {
    process.stdout.write("\n--- per-cell diagnostics ---\n");
    for (const row of result.diagnostics) {
      for (const d of row) {
        process.stdout.write(
          `(${d.x},${d.y}) ${d.status.padEnd(8)} ` +
            `bevel=${d.beveled ? "Y" : "n"} ` +
            `glyph=${String(d.glyphPixels).padStart(3)} ` +
            `blk=${String(d.blackPixels).padStart(3)} ` +
            `red=${String(d.redPixels).padStart(3)} ` +
            `bgR=${d.bgRedness.toFixed(0).padStart(3)} ` +
            `win=${d.winnerRef ?? "-"} ` +
            `mean=(${d.centerMean.r.toFixed(0)},${d.centerMean.g.toFixed(0)},${d.centerMean.b.toFixed(0)})\n`,
        );
      }
    }
  }

  return 0;
}

if (require.main === module) {
  process.exit(main(process.argv));
}
