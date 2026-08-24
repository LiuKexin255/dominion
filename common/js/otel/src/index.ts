import { register } from "node:module";

import { context, metrics, trace } from "@opentelemetry/api";
import { logs } from "@opentelemetry/api-logs";
import { OTLPLogExporter } from "@opentelemetry/exporter-logs-otlp-http";
import { OTLPMetricExporter } from "@opentelemetry/exporter-metrics-otlp-http";
import { OTLPTraceExporter } from "@opentelemetry/exporter-trace-otlp-http";
import type { Instrumentation } from "@opentelemetry/instrumentation";
import { registerInstrumentations } from "@opentelemetry/instrumentation";
import { resourceFromAttributes } from "@opentelemetry/resources";
import type { Resource } from "@opentelemetry/resources";
import {
	BatchLogRecordProcessor,
	LoggerProvider,
} from "@opentelemetry/sdk-logs";
import {
	MeterProvider,
	PeriodicExportingMetricReader,
} from "@opentelemetry/sdk-metrics";
import {
	AlwaysOnSampler,
	BatchSpanProcessor,
} from "@opentelemetry/sdk-trace-base";
import { NodeTracerProvider } from "@opentelemetry/sdk-trace-node";
import { ATTR_SERVICE_NAME } from "@opentelemetry/semantic-conventions";

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

/** Default OTel collector HTTP endpoint (port 4318, not gRPC 4317). */
const OTEL_COLLECTOR_URL =
	"http://dominion-opentelemetry-collector.kube-public.svc.cluster.local:4318";

/** Tracer and meter name used to obtain instruments from global providers. */
const SERVICE_NAME = "dominion";

// ---------------------------------------------------------------------------
// State
// ---------------------------------------------------------------------------

let _tracerProvider: NodeTracerProvider | null = null;
let _meterProvider: MeterProvider | null = null;
let _loggerProvider: LoggerProvider | null = null;
let _loggerProviderSet = false;
let _initialized = false;
// module.register() hooks cannot be deregistered, so this flag survives
// shutdown() — repeated init()/shutdown() cycles must not stack hook layers.
let _esmHookRegistered = false;

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/**
 * Returns true when ALL three deploy environment variables are present and
 * non-empty: `SERVICE_APP`, `DOMINION_ENVIRONMENT`, `POD_NAMESPACE`.
 *
 * Matches the Go `isDeploy()` behaviour in `common/gopkg/otel/otel.go`.
 */
function isDeploy(): boolean {
	return !!(
		process.env.SERVICE_APP &&
		process.env.DOMINION_ENVIRONMENT &&
		process.env.POD_NAMESPACE
	);
}

/**
 * Build a Resource with the default service name attribute.
 *
 * Uses `resourceFromAttributes()` (the v2.x API) instead of
 * `new Resource()` which is type-only in OTel SDK ≥ 2.
 */
function buildResource(): Resource {
	return resourceFromAttributes({ [ATTR_SERVICE_NAME]: SERVICE_NAME });
}

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/** Configuration object accepted by `init()`. */
export interface OtelConfig {
	/**
	 * Array of @opentelemetry/instrumentation instances to register.
	 *
	 * Passed directly to `registerInstrumentations()`.  Consumer is responsible
	 * for ensuring instrumentation modules are loaded before any instrumented
	 * library (e.g. `@grpc/grpc-js`) is imported.
	 */
	instrumentations?: Instrumentation[];
}

/**
 * Initialize OpenTelemetry providers.
 *
 * On the first call, registers the OTel ESM loader hook (IITM) before any
 * instrumented module can be loaded: RITM (require-in-the-middle) patches
 * `Module.prototype.require`, which Node's ESM loader bypasses, so CJS
 * packages (e.g. `@grpc/grpc-js`) imported from ESM are only instrumented via
 * the hook registered here. Timing contract: `init()` (and thus the hook
 * registration) must complete before a bootstrap dynamically imports its
 * server module — see
 * specs/048-js-esm-migration/contracts/otel-instrumentation-esm-contract.md §2.
 *
 * Behaviour depends on the deployment environment:
 *
 * **Deploy mode** (all three env vars `SERVICE_APP`, `DOMINION_ENVIRONMENT`,
 * `POD_NAMESPACE` are set):
 *   - Creates `NodeTracerProvider` with OTLP HTTP exporter (batch).
 *   - Creates `MeterProvider` with OTLP HTTP periodic metric reader.
 *   - Creates `LoggerProvider` with OTLP HTTP batch log processor.
 *   - All exporters target
 *     `http://dominion-opentelemetry-collector.kube-public.svc.cluster.local:4318`.
 *
 * **Non-deploy mode** (any env var missing):
 *   - Creates local-only `NodeTracerProvider` with `AlwaysOnSampler`
 *     and no remote export.
 *   - Creates local-only `MeterProvider` with no remote export.
 *   - Creates local-only `LoggerProvider` with no remote export.
 *
 * `init()` is **idempotent** — subsequent calls after a successful
 * first call return immediately without re-registering providers.
 *
 * @param config - Optional configuration including instrumentation modules.
 */
export async function init(config?: OtelConfig): Promise<void> {
	if (_initialized) {
		return;
	}

	// ---- ESM instrumentation hook --------------------------------------
	// parentURL is this module's own URL; @opentelemetry/instrumentation is
	// a direct dependency of this package, so hook.mjs resolves reliably in
	// both the Bazel runfiles tree and the flattened npm closure in a
	// service tar.
	if (!_esmHookRegistered) {
		register("@opentelemetry/instrumentation/hook.mjs", import.meta.url);
		_esmHookRegistered = true;
	}

	const deploy = isDeploy();
	const resource = buildResource();

	// ---- TracerProvider ------------------------------------------------
	if (deploy) {
		_tracerProvider = new NodeTracerProvider({
			resource,
			spanProcessors: [
				new BatchSpanProcessor(
					new OTLPTraceExporter({
						url: `${OTEL_COLLECTOR_URL}/v1/traces`,
					}),
				),
			],
		});
	} else {
		_tracerProvider = new NodeTracerProvider({
			resource,
			sampler: new AlwaysOnSampler(),
		});
	}
	_tracerProvider.register();

	// ---- MeterProvider -------------------------------------------------
	if (deploy) {
		_meterProvider = new MeterProvider({
			resource,
			readers: [
				new PeriodicExportingMetricReader({
					exporter: new OTLPMetricExporter({
						url: `${OTEL_COLLECTOR_URL}/v1/metrics`,
					}),
				}),
			],
		});
	} else {
		_meterProvider = new MeterProvider({ resource });
	}

	// ---- LoggerProvider ------------------------------------------------
	if (deploy) {
		_loggerProvider = new LoggerProvider({
			resource,
			processors: [
				new BatchLogRecordProcessor(
					new OTLPLogExporter({
						url: `${OTEL_COLLECTOR_URL}/v1/logs`,
					}),
				),
			],
		});
	} else {
		_loggerProvider = new LoggerProvider({ resource });
	}
	_loggerProviderSet = true;
	logs.setGlobalLoggerProvider(_loggerProvider);

	// ---- Instrumentations ----------------------------------------------
	if (config?.instrumentations && config.instrumentations.length > 0) {
		registerInstrumentations({
			instrumentations: config.instrumentations,
		});
	}

	_initialized = true;
}

/**
 * Return a `Tracer` from the global `TracerProvider`.
 *
 * The tracer is always obtained via `trace.getTracer("dominion")`.
 * Must be called after `init()`; if called before, the tracer obtained
 * from the default no-op `TracerProvider` will produce no spans.
 */
export function tracer(): ReturnType<typeof trace.getTracer> {
	return trace.getTracer(SERVICE_NAME);
}

/**
 * Return a `Meter` from the global `MeterProvider`.
 *
 * The meter is always obtained via `metrics.getMeter("dominion")`.
 * Must be called after `init()`; if called before, the meter obtained
 * from the default no-op `MeterProvider` will produce no metrics.
 */
export function meter(): ReturnType<typeof metrics.getMeter> {
	return metrics.getMeter(SERVICE_NAME);
}

/**
 * Return the trace ID of the active span as a 32-char hex string.
 *
 * When no span is active in the current context, returns an empty
 * string `""`.
 */
export function traceId(): string {
	const span = trace.getSpan(context.active());
	if (!span) {
		return "";
	}
	return span.spanContext().traceId;
}

/**
 * Reports whether `init()` has been called and the `LoggerProvider` is set.
 *
 * After a successful `init()` call this returns `true` regardless of deploy
 * vs. non-deploy mode.  Before `init()` it returns `false`.
 */
export function isLoggerProviderSet(): boolean {
	return _loggerProviderSet;
}

/**
 * Shut down all OpenTelemetry providers **sequentially** in the order:
 * `TracerProvider` → `MeterProvider` → `LoggerProvider`.
 *
 * - Each provider's internal state is set to `null` after its shutdown
 *   completes.
 * - `isLoggerProviderSet()` will return `false` afterward.
 * - If `init()` was never called (no providers exist) this function
 *   resolves immediately without error.
 * - Safe to call multiple times; subsequent calls are silent no-ops.
 */
export async function shutdown(): Promise<void> {
	if (_tracerProvider) {
		await _tracerProvider.shutdown();
		_tracerProvider = null;
	}
	if (_meterProvider) {
		await _meterProvider.shutdown();
		_meterProvider = null;
	}
	if (_loggerProvider) {
		await _loggerProvider.shutdown();
		_loggerProvider = null;
	}
	_loggerProviderSet = false;
	_initialized = false;
}
