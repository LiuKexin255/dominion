import { afterEach, beforeEach, describe, expect, it } from "vitest";
import {
	init,
	isLoggerProviderSet,
	meter,
	shutdown,
	traceId,
	tracer,
} from "./index";

// ---- helpers ----------------------------------------------------------------

/** Reset env vars that were modified during deploy-mode tests. */
function clearDeployEnv() {
	delete process.env.SERVICE_APP;
	delete process.env.DOMINION_ENVIRONMENT;
	delete process.env.POD_NAMESPACE;
}

// ---- lifecycle --------------------------------------------------------------

beforeEach(async () => {
	// Ensure clean state: any previous test's providers are shut down.
	await shutdown();
	clearDeployEnv();
});

afterEach(async () => {
	await shutdown();
	clearDeployEnv();
});

// ============================================================================
// isLoggerProviderSet
// ============================================================================
describe("isLoggerProviderSet", () => {
	it("returns false before init()", () => {
		expect(isLoggerProviderSet()).toBe(false);
	});

	it("returns true after init()", async () => {
		await init();
		expect(isLoggerProviderSet()).toBe(true);
	});

	it("returns false after shutdown()", async () => {
		await init();
		await shutdown();
		expect(isLoggerProviderSet()).toBe(false);
	});
});

// ============================================================================
// init
// ============================================================================
describe("init", () => {
	it("succeeds in non-deploy mode (default)", async () => {
		// No env vars set → non-deploy path.
		await expect(init()).resolves.toBeUndefined();
		expect(isLoggerProviderSet()).toBe(true);
	});

	it("is idempotent — second call is a no-op", async () => {
		await init();
		expect(isLoggerProviderSet()).toBe(true);

		// Second call should return immediately without error.
		await expect(init()).resolves.toBeUndefined();
		expect(isLoggerProviderSet()).toBe(true);
	});

	it("succeeds in deploy mode when all three env vars are set", async () => {
		process.env.SERVICE_APP = "test-app";
		process.env.DOMINION_ENVIRONMENT = "test-env";
		process.env.POD_NAMESPACE = "test-ns";
		await expect(init()).resolves.toBeUndefined();
		expect(isLoggerProviderSet()).toBe(true);
	});

	it("falls back to non-deploy when only some env vars are set", async () => {
		process.env.SERVICE_APP = "test-app";
		// DOMINION_ENVIRONMENT and POD_NAMESPACE missing.
		await expect(init()).resolves.toBeUndefined();
		expect(isLoggerProviderSet()).toBe(true);
	});

	it("supports empty instrumentations array without error", async () => {
		await expect(init({ instrumentations: [] })).resolves.toBeUndefined();
		expect(isLoggerProviderSet()).toBe(true);
	});

	it("accepts undefined config", async () => {
		await expect(init(undefined)).resolves.toBeUndefined();
		expect(isLoggerProviderSet()).toBe(true);
	});
});

// ============================================================================
// tracer
// ============================================================================
describe("tracer", () => {
	it("returns a Tracer after init()", async () => {
		await init();
		const t = tracer();
		expect(t).toBeDefined();
		expect(typeof t.startSpan).toBe("function");
		expect(typeof t.startActiveSpan).toBe("function");
	});
});

// ============================================================================
// meter
// ============================================================================
describe("meter", () => {
	it("returns a Meter after init()", async () => {
		await init();
		const m = meter();
		expect(m).toBeDefined();
		expect(typeof m.createCounter).toBe("function");
		expect(typeof m.createHistogram).toBe("function");
	});
});

// ============================================================================
// traceId
// ============================================================================
describe("traceId", () => {
	it("returns empty string when no span is active", async () => {
		// init() registers providers but does not start a span.
		await init();
		expect(traceId()).toBe("");
	});

	it("returns empty string before init()", () => {
		expect(traceId()).toBe("");
	});
});

// ============================================================================
// shutdown
// ============================================================================
describe("shutdown", () => {
	it("resolves silently when init() was never called", async () => {
		// No providers exist; should not throw.
		await expect(shutdown()).resolves.toBeUndefined();
	});

	it("shuts down providers created by init()", async () => {
		await init();
		expect(isLoggerProviderSet()).toBe(true);
		await shutdown();
		expect(isLoggerProviderSet()).toBe(false);
	});

	it("is idempotent — second shutdown resolves silently", async () => {
		await init();
		await shutdown();
		await expect(shutdown()).resolves.toBeUndefined();
		expect(isLoggerProviderSet()).toBe(false);
	});

	it("allows re-init after shutdown", async () => {
		await init();
		await shutdown();
		expect(isLoggerProviderSet()).toBe(false);

		await init();
		expect(isLoggerProviderSet()).toBe(true);
	});
});
