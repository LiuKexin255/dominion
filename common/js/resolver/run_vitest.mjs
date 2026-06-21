import { startVitest } from "vitest/node";

async function main() {
  const args = process.argv.slice(2);
  const result = await startVitest(args);
  if (!result) {
    process.exit(1);
  }
}

main().catch((err) => {
  console.error(err);
  process.exit(1);
});
