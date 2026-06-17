/**
 * server.ts — Experimental OpenAI LLM client service.
 *
 * Exposes an HTTP endpoint that invokes @langchain/openai's ChatOpenAI
 * against a configurable fake/base LLM endpoint and returns the raw parsed
 * content blocks plus any reasoning_content found in additional_kwargs.
 */

import * as http from "node:http";
import { ChatOpenAI } from "@langchain/openai";
import { concat } from "@langchain/core/utils/stream";
import type { AIMessageChunk } from "@langchain/core/messages";
import { createResolver } from "@dominion/common-js-resolver";
import { info } from "@dominion/common-js-logs";

const port = process.env.PORT || "8080";
const fakeTarget = process.env.FAKE_LLM_TARGET || "openllm/fake-service:8080";

const resolver = createResolver();

interface InvokeRequest {
  prompt?: string;
}

interface InvokeResponse {
  baseURL: string;
  blocks: Array<{ type: string; [key: string]: unknown }>;
  additionalKwargs: {
    reasoning_content: string | null;
  };
  hasNativeReasoningBlock: boolean;
}

async function resolveFakeBaseURL(): Promise<string> {
  const endpoints = await resolver.resolve(fakeTarget);
  if (endpoints.length === 0) {
    throw new Error(`no endpoints resolved for target ${fakeTarget}`);
  }
  const baseURL = `http://${endpoints[0]}/v1`;
  info("resolved fake service endpoint", { target: fakeTarget, baseURL });
  return baseURL;
}

async function handleInvoke(req: http.IncomingMessage, res: http.ServerResponse): Promise<void> {
  if (req.method !== "POST") {
    res.writeHead(405).end("method not allowed");
    return;
  }

  const body = await readBody(req);
  let parsed: InvokeRequest;
  try {
    parsed = JSON.parse(body) as InvokeRequest;
  } catch (err) {
    res.writeHead(400).end(`invalid json: ${err}`);
    return;
  }

  const prompt = parsed.prompt || "say hello and think out loud";
  const baseURL = await resolveFakeBaseURL();

  info("invoke: calling ChatOpenAI via resolver", { prompt, baseURL });

  const model = new ChatOpenAI({
    model: "any-model",
    streaming: true,
    configuration: {
      baseURL,
      apiKey: "dummy",
    },
  });

  const stream = await model.stream([{ role: "user", content: prompt }]);
  let full: AIMessageChunk | undefined;
  for await (const chunk of stream) {
    full = full ? (concat(full, chunk) as AIMessageChunk) : chunk;
  }

  if (!full) {
    res.writeHead(500).end("no response from model");
    return;
  }

  const blocks = Array.isArray(full.contentBlocks) ? full.contentBlocks : [];
  const reasoningFromKwargs =
    typeof full.additional_kwargs?.reasoning_content === "string"
      ? (full.additional_kwargs.reasoning_content as string)
      : null;
  const hasNativeReasoningBlock = blocks.some(
    (b: { type?: string }) => b.type === "reasoning",
  );

  info("invoke: result", {
    blockCount: blocks.length,
    hasNativeReasoningBlock,
    hasReasoningInKwargs: reasoningFromKwargs !== null,
  });

  const response: InvokeResponse = {
    baseURL,
    blocks,
    additionalKwargs: {
      reasoning_content: reasoningFromKwargs,
    },
    hasNativeReasoningBlock,
  };

  res.writeHead(200, { "Content-Type": "application/json" });
  res.end(JSON.stringify(response));
}

function readBody(req: http.IncomingMessage): Promise<string> {
  return new Promise((resolve, reject) => {
    const chunks: Buffer[] = [];
    req.on("data", (chunk) => chunks.push(chunk));
    req.on("end", () => resolve(Buffer.concat(chunks).toString("utf-8")));
    req.on("error", reject);
  });
}

export async function startServer(): Promise<http.Server> {
  const server = http.createServer((req, res) => {
    if (req.url === "/invoke" || req.url === "/experimental/openai-llm/invoke") {
      handleInvoke(req, res).catch((err) => {
        console.error("handleInvoke error:", err);
        if (!res.headersSent) {
          res.writeHead(500).end("internal error");
        }
      });
      return;
    }
    if (req.url === "/health" || req.url === "/experimental/openai-llm/health") {
      res.writeHead(200).end("ok");
      return;
    }
    res.writeHead(404).end("not found");
  });

  return new Promise((resolve) => {
    server.listen(Number(port), () => {
      console.error("[server] openai-llm-client listening on :%s", port);
      info("server listening", { port, fakeTarget });
      resolve(server);
    });
  });
}
