import { describe, it, expect, beforeAll, afterAll } from "vitest";
import * as path from "node:path";
import { NodeTracerProvider } from "@opentelemetry/sdk-trace-node";
import {
  InMemorySpanExporter,
  SimpleSpanProcessor,
} from "@opentelemetry/sdk-trace-base";
import { registerInstrumentations } from "@opentelemetry/instrumentation";
import { createGrpcInstrumentation } from "./index";

// ============================================================================
// CRITICAL LOAD ORDER: registerInstrumentations() MUST run BEFORE
// require("@grpc/grpc-js"). The instrumentation installs a require-in-the-
// middle hook on Node's Module._load; the subsequent require() triggers that
// hook so the module is patched.
//
// vitest's import() bypasses Module._load entirely (Vite SSR loader), so the
// module would load UNPATCHED and produce zero spans. Only require() goes
// through the hook. See specs/019-js-test-reliability/research.md §6.
// ============================================================================
const exporter = new InMemorySpanExporter();
const provider = new NodeTracerProvider({
  spanProcessors: [new SimpleSpanProcessor(exporter)],
});

provider.register();
registerInstrumentations({
  instrumentations: [createGrpcInstrumentation()],
});

// Loaded via require() AFTER registration so the hook fires and patches it.
const grpc: typeof import("@grpc/grpc-js") = require("@grpc/grpc-js");
const protoLoader: typeof import("@grpc/proto-loader") = require("@grpc/proto-loader");

// ---- Proto & service setup --------------------------------------------------

function getProtoPackage(): any {
  const protoPath = path.join(__dirname, "test_service.proto");
  const pkgDef = protoLoader.loadSync(protoPath, {
    keepCase: true,
    longs: String,
    enums: String,
    defaults: true,
    oneofs: true,
  });
  return grpc.loadPackageDefinition(pkgDef);
}

async function getSpans(
  minCount: number,
  timeoutMs: number = 5000,
): Promise<any[]> {
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    // InMemorySpanExporter exposes getFinishedSpans(), not getFinishedSpanItems()
    // (https://github.com/open-telemetry/opentelemetry-js — sdk-trace-base InMemorySpanExporter).
    const finished = exporter.getFinishedSpans();
    if (finished.length >= minCount) {
      return finished;
    }
    await new Promise((r) => setTimeout(r, 50));
  }
  const last = exporter.getFinishedSpans();
  return last;
}

// ---- Service implementations ------------------------------------------------

const testService = {
  unaryCall: (call: any, callback: any) => {
    callback(null, { message: `Hello ${call.request.message}` });
  },
  serverStream: (call: any) => {
    call.write({ message: "stream-1" });
    call.write({ message: "stream-2" });
    call.end();
  },
  clientStream: (call: any, callback: any) => {
    const messages: string[] = [];
    call.on("data", (req: any) => messages.push(req.message));
    call.on("end", () => callback(null, { message: messages.join(",") }));
  },
  bidiStream: (call: any) => {
    call.on("data", (req: any) => {
      call.write({ message: `Echo: ${req.message}` });
    });
    call.on("end", () => call.end());
  },
};

// ---- Server & client lifecycle ----------------------------------------------

let server: import("@grpc/grpc-js").Server;
let client: any;

beforeAll(async () => {
  exporter.reset();

  const proto = getProtoPackage();
  server = new grpc.Server();
  server.addService(proto.test.TestService.service, testService);
  const bindPort = await new Promise<number>((resolve, reject) => {
    server.bindAsync(
      "0.0.0.0:0",
      grpc.ServerCredentials.createInsecure(),
      (err: any, port: number) => {
        if (err) reject(err);
        else resolve(port);
      },
    );
  });
  client = new proto.test.TestService(
    `localhost:${bindPort}`,
    grpc.credentials.createInsecure(),
  );
});

afterAll(() => {
  server?.forceShutdown();
  client?.close();
});

// ---- Helper to verify common RPC span attributes ----------------------------

function collectRpcAttributes(spans: any[]): any[] {
  return spans
    .filter(
      (s) =>
        s.attributes["rpc.system"] === "grpc" ||
        s.attributes["rpc.service"] === "test.TestService" ||
        s.attributes["rpc.method"] !== undefined,
    )
    .map((s) => ({
      name: s.name,
      kind: s.kind,
      rpcSystem: s.attributes["rpc.system"],
      rpcService: s.attributes["rpc.service"],
      rpcMethod: s.attributes["rpc.method"],
      statusCode: s.attributes["rpc.grpc.status_code"],
      parentSpanId: s.parentSpanContext?.spanId,
      spanId: s.spanContext().spanId,
    }));
}

// ============================================================================
// Tests
// ============================================================================

// Root cause of the original 0-spans failure (specs/019): vitest's import()
// for CJS node_modules bypasses Node's Module._load, where OTel's
// require-in-the-middle installs its monkey-patch hook. The module loaded
// unpatched → zero spans. Fix: use require() AFTER registerInstrumentations()
// so the hook fires. See research.md §6.
describe("gRPC Instrumentation", () => {
  describe("UnaryCall", () => {
    it("creates client and server spans with correct attributes", async () => {
      exporter.reset();
      const response = await new Promise<any>((resolve, reject) => {
        client.unaryCall({ message: "world" }, (err: any, res: any) => {
          if (err) reject(err);
          else resolve(res);
        });
      });
      expect(response.message).toBe("Hello world");

      const spans = await getSpans(2);
      const rpcSpans = collectRpcAttributes(spans);

      expect(rpcSpans.length).toBeGreaterThanOrEqual(2);

      // Verify client span
      const clientSpan = rpcSpans.find((s) => s.kind === 2); // SpanKind.CLIENT
      expect(clientSpan).toBeDefined();
      expect(clientSpan!.rpcSystem).toBe("grpc");
      expect(clientSpan!.rpcService).toBe("test.TestService");
      expect(clientSpan!.rpcMethod).toBe("UnaryCall");
      expect(clientSpan!.statusCode).toBe(0);

      // Verify server span
      const serverSpan = rpcSpans.find((s) => s.kind === 1); // SpanKind.SERVER
      expect(serverSpan).toBeDefined();
      expect(serverSpan!.rpcSystem).toBe("grpc");
      expect(serverSpan!.rpcService).toBe("test.TestService");
      expect(serverSpan!.rpcMethod).toBe("UnaryCall");
      expect(serverSpan!.statusCode).toBe(0);

      // Verify parent-child linkage: the server span's parent should be the
      // client span (client injects trace context into gRPC metadata).
      expect(serverSpan!.parentSpanId).toBe(clientSpan!.spanId);
    });
  });

  describe("ServerStream", () => {
    it("creates spans for server streaming RPC", async () => {
      exporter.reset();
      const messages: string[] = [];
      const call = client.serverStream({ message: "start" });
      call.on("data", (res: any) => messages.push(res.message));
      await new Promise<void>((resolve) => call.on("end", resolve));

      expect(messages).toEqual(["stream-1", "stream-2"]);

      const spans = await getSpans(1);
      const rpcSpans = collectRpcAttributes(spans);

      expect(rpcSpans.length).toBeGreaterThanOrEqual(1);

      // At minimum we should see client spans (and possibly server spans)
      const methodSpans = rpcSpans.filter(
        (s) => s.rpcMethod === "ServerStream",
      );
      expect(methodSpans.length).toBeGreaterThanOrEqual(1);
    });
  });

  describe("ClientStream", () => {
    it("creates spans for client streaming RPC", async () => {
      exporter.reset();
      const response = await new Promise<any>((resolve, reject) => {
        const call = client.clientStream((err: any, res: any) => {
          if (err) reject(err);
          else resolve(res);
        });
        call.write({ message: "a" });
        call.write({ message: "b" });
        call.end();
      });
      expect(response.message).toBe("a,b");

      const spans = await getSpans(1);
      const rpcSpans = collectRpcAttributes(spans);

      expect(rpcSpans.length).toBeGreaterThanOrEqual(1);

      const methodSpans = rpcSpans.filter(
        (s) => s.rpcMethod === "ClientStream",
      );
      expect(methodSpans.length).toBeGreaterThanOrEqual(1);
    });
  });

  describe("BidiStream", () => {
    it("creates spans for bidirectional streaming RPC", async () => {
      exporter.reset();
      const messages: string[] = [];
      const call = client.bidiStream();
      call.on("data", (res: any) => messages.push(res.message));
      call.write({ message: "hello" });
      call.write({ message: "world" });
      call.end();
      await new Promise<void>((resolve) => call.on("end", resolve));

      expect(messages).toContain("Echo: hello");
      expect(messages).toContain("Echo: world");

      const spans = await getSpans(1);
      const rpcSpans = collectRpcAttributes(spans);

      expect(rpcSpans.length).toBeGreaterThanOrEqual(1);

      const methodSpans = rpcSpans.filter(
        (s) => s.rpcMethod === "BidiStream",
      );
      expect(methodSpans.length).toBeGreaterThanOrEqual(1);
    });
  });
});
