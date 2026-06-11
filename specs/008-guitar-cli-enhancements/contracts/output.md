# Contract: Guitar Run Console Output

## Goals

Output must make suite boundaries, steps, and results visible during multi-suite runs while remaining clean when redirected to files or pipes.

## Suite section format

Each executed suite prints a section with:

1. A title/header containing the suite name.
2. Suite metadata including run ID, environment name, and deploy path when available.
3. Indented step lines for deploy, test, and cleanup.
4. A final status line for success or failure.

Exact glyphs are implementation-defined, but tests must assert the semantic content:

- suite name appears in the header
- deploy/test/cleanup steps are indented relative to the header
- success output names the successful suite
- failure output names the failed suite and failed step/error

## Color behavior

When output supports color:

- Running/in-progress headers may use the default color or yellow.
- Successful suite status uses green.
- Failed suite status and error emphasis use red.
- Different suites may rotate through a small palette for headers, but status colors take precedence.

When output does not support color:

- No ANSI escape sequences are emitted.
- Text still includes suite names, indentation, steps, and status words.

## Color disable conditions

Color must be disabled when any of these is true:

- The output writer is not a terminal, such as a pipe/file used by `guitar run <plan.yaml> | tee output.log`.
- `TERM` is empty or `TERM=dumb`.

## Failure and cleanup output

- If deploy or test fails, cleanup still runs with the existing `context.WithoutCancel` behavior.
- If cleanup fails after another failure, the returned error includes both the original failure and cleanup failure.
- If cleanup fails after suite success, a warning is printed and the run continues according to existing behavior.
