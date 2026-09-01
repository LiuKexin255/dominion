/**
 * server.ts — HTTP endpoint that runs the team graph end-to-end.
 *
 * Mirrors experimental/openai_llm/client/src/server.ts: the LLM model is a
 * ChatOpenAI whose baseURL is resolved from the deployed fake-llm target via
 * @dominion/common-js-resolver (A5 — proves ChatOpenAI → fake-llm can drive
 * the team graph). The endpoint runs the graph and returns the final
 * gameEnded, the persisted strategy, and per-agent message counts.
 */

import * as http from "node:http";
import { ChatOpenAI } from "@langchain/openai";
import { HumanMessage } from "@langchain/core/messages";
import { createResolver } from "@dominion/common-js-resolver";
import { info } from "@dominion/common-js-logs";
import {
  InMemoryStrategyStore,
  GameEventBuffer,
  buildTeamGraph,
} from "./team-graph.js";

const port = process.env.PORT || "8080";
const fakeTarget = process.env.FAKE_LLM_TARGET || "game/fake-llm:8080";
const resolver = createResolver();

interface InvokeRequest {
  prompt?: string;
  sessionId?: string;
}

interface InvokeResponse {
  baseURL: string;
  gameEnded: string | null;
  strategy: string;
  playerMessageCount: number;
  plannerMessageCount: number;
}

async function resolveFakeBaseURL(): Promise<string> {
  const endpoints = await resolver.resolve(fakeTarget);
  if (endpoints.length === 0) {
    throw new Error(`no endpoints resolved for target ${fakeTarget}`);
  }
  const baseURL = `http://${endpoints[0]}/v1`;
  info("resolved fake-llm endpoint", { target: fakeTarget, baseURL });
  return baseURL;
}

async function handleInvoke(
  req: http.IncomingMessage,
  res: http.ServerResponse,
): Promise<void> {
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

  const prompt = parsed.prompt || "play one move";
  const sessionId = parsed.sessionId || "spike-session";
  const baseURL = await resolveFakeBaseURL();

  info("invoke: running team graph via ChatOpenAI", { prompt, baseURL });

  // A5: ChatOpenAI pointed at the OpenAI-compatible fake-llm drives BOTH
  // agents. One ChatOpenAI instance is reused for player + planner (the
  // fake-llm is stateless across requests; each agent invokes independently).
  const model = new ChatOpenAI({
    model: "any-model",
    configuration: { baseURL, apiKey: "dummy" },
  });

  const strategyStore = new InMemoryStrategyStore();
  const sink = new GameEventBuffer();
  const { graph } = buildTeamGraph({
    playerModel: model,
    plannerModel: model,
    strategyStore,
    sink,
    sessionId,
  });

  const threadId = `spike-${sessionId}`;
  const result = (await graph.invoke(
    { playerMessages: [new HumanMessage(prompt)] },
    { configurable: { thread_id: threadId }, recursionLimit: 50 },
  )) as {
    playerMessages: unknown[];
    plannerMessages: unknown[];
    gameEnded: string | null;
  };

  const strategy = await strategyStore.get(sessionId);
  const response: InvokeResponse = {
    baseURL,
    gameEnded: result.gameEnded,
    strategy,
    playerMessageCount: Array.isArray(result.playerMessages)
      ? result.playerMessages.length
      : 0,
    plannerMessageCount: Array.isArray(result.plannerMessages)
      ? result.plannerMessages.length
      : 0,
  };

  info("invoke: result", {
    gameEnded: response.gameEnded,
    strategyLen: response.strategy.length,
    playerMsgs: response.playerMessageCount,
    plannerMsgs: response.plannerMessageCount,
  });

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
    if (
      req.url === "/invoke" ||
      req.url === "/experimental/team-graph/invoke"
    ) {
      handleInvoke(req, res).catch((err) => {
        console.error("handleInvoke error:", err);
        if (!res.headersSent) {
          res.writeHead(500).end("internal error");
        }
      });
      return;
    }
    if (
      req.url === "/health" ||
      req.url === "/experimental/team-graph/health"
    ) {
      res.writeHead(200).end("ok");
      return;
    }
    res.writeHead(404).end("not found");
  });

  return new Promise((resolve) => {
    server.listen(Number(port), () => {
      console.error("[server] team-graph-spike listening on :%s", port);
      info("server listening", { port, fakeTarget });
      resolve(server);
    });
  });
}
