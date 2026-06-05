import { type GrpcInstrumentationConfig, GrpcInstrumentation } from "@opentelemetry/instrumentation-grpc";

export type { GrpcInstrumentationConfig };
export { GrpcInstrumentation };

/**
 * Creates a GrpcInstrumentation instance with sensible defaults.
 *
 * **CRITICAL LOAD-ORDERING REQUIREMENT**: This instrumentation MUST be registered
 * via `registerInstrumentations()` from `@opentelemetry/instrumentation` BEFORE
 * `@grpc/grpc-js` is loaded in your application. If `@grpc/grpc-js` is imported
 * before instrumentation registration, the instrumentation cannot patch gRPC-JS
 * and no spans will be produced.
 *
 * **Recommended pattern**:
 * ```typescript
 * import { init } from "@dominion/common-js-otel";
 * import { createGrpcInstrumentation } from "@dominion/common-js-grpc-otel";
 *
 * await init({ instrumentations: [createGrpcInstrumentation()] });
 *
 * // Only now import server which loads @grpc/grpc-js
 * const server = await import("./server.js");
 * ```
 *
 * @param config - Optional GrpcInstrumentation configuration. Merged over defaults.
 * @returns Configured GrpcInstrumentation instance ready for use with init().
 */
export function createGrpcInstrumentation(
  config?: Partial<GrpcInstrumentationConfig>,
): GrpcInstrumentation {
  const defaults: GrpcInstrumentationConfig = {
    ignoreGrpcMethods: [],
    metadataToSpanAttributes: {},
  };
  return new GrpcInstrumentation({ ...defaults, ...config });
}
