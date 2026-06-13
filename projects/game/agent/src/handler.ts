/**
 * handler.ts — AgentServiceServer gRPC handler implementations.
 *
 * Implements CreateAgent, GetAgent, DeleteAgent, and Connect RPCs
 * for the AgentService defined in game.proto.
 */

import * as grpc from "@grpc/grpc-js";
import { randomUUID } from "node:crypto";
import { info, warn, error } from "@dominion/common-js-logs";

import type { AgentServiceHandlers } from "../game_types/projects/game/AgentService";
import type { Agent as AgentMessage } from "../game_types/projects/game/Agent";
import type { AgentFrame } from "../game_types/projects/game/AgentFrame";

import type { LLMAdapter } from "./llm";
import type { PromptClient } from "./prompt-client";
import { DialogRuntime } from "./runtime";

// FrameSender enum values duplicated from proto because generated game_types/
// only provides .ts type files (not compiled .js); importing a runtime value
// would fail at require() time.
const FrameSender = {
  FRAME_SENDER_UNSPECIFIED: "FRAME_SENDER_UNSPECIFIED",
  FRAME_SENDER_USER: "FRAME_SENDER_USER",
  FRAME_SENDER_AGENT: "FRAME_SENDER_AGENT",
  FRAME_SENDER_SYSTEM: "FRAME_SENDER_SYSTEM",
} as const;

// ---------------------------------------------------------------------------
// Handler
// ---------------------------------------------------------------------------

export class Handler implements AgentServiceHandlers {
  /** Index signature required by grpc.UntypedServiceImplementation. */
  [name: string]: any;

  /** Active runtime instances keyed by session ID. */
  private instances: Map<string, DialogRuntime>;

  /** Prompt service client for fetching agent profiles. */
  private promptClient: PromptClient;

  /** LLM adapter for generating agent responses. */
  private llmAdapter: LLMAdapter;

  /** Provider API key / secret. */
  private providerSecret: string;

  constructor(
    instances: Map<string, DialogRuntime>,
    promptClient: PromptClient,
    llmAdapter: LLMAdapter,
    providerSecret: string,
  ) {
    this.instances = instances;
    this.promptClient = promptClient;
    this.llmAdapter = llmAdapter;
    this.providerSecret = providerSecret;
  }

  // -----------------------------------------------------------------------
  // CreateAgent
  // -----------------------------------------------------------------------

  CreateAgent: grpc.handleUnaryCall<
    { sessionId?: string; agentProfileName?: string },
    AgentMessage
  > = async (call, callback) => {
    const sessionId = call.request.sessionId ?? "";
    const profileName = call.request.agentProfileName ?? "";

    if (!profileName) {
      return callback({
        code: grpc.status.NOT_FOUND,
        details: "agent_profile_name is required",
      } as grpc.ServiceError);
    }

    try {
      const profile = await this.promptClient.getProfile(profileName);
      const runtime = DialogRuntime.createWithProfile(
        sessionId,
        profileName,
        profile.model,
        profile.systemPrompt,
      );
      this.instances.set(sessionId, runtime);

      const agent: AgentMessage = {
        name: `sessions/${sessionId}/agent`,
        sessionId,
        agentProfileName: profileName,
        createTime: timestampNow(),
      };

      callback(null, agent);
    } catch (err: unknown) {
      const details =
        err instanceof Error ? err.message : "Failed to create agent";
      error("create agent failed", { sessionId, profileName, error: details });
      const code =
        err instanceof Error && "code" in err
          ? (err as grpc.ServiceError).code
          : grpc.status.INTERNAL;
      callback({
        code:
          code === grpc.status.NOT_FOUND
            ? grpc.status.NOT_FOUND
            : grpc.status.INTERNAL,
        details:
          err instanceof Error ? err.message : "Failed to create agent",
      } as grpc.ServiceError);
    }
  };

  // -----------------------------------------------------------------------
  // GetAgent
  // -----------------------------------------------------------------------

  GetAgent: grpc.handleUnaryCall<{ sessionId?: string }, AgentMessage> = (
    call,
    callback,
  ) => {
    const sessionId = call.request.sessionId ?? "";
    const runtime = this.instances.get(sessionId);

    if (!runtime) {
      return callback({
        code: grpc.status.NOT_FOUND,
        details: `Agent not found for session: ${sessionId}`,
      } as grpc.ServiceError);
    }

    const agent: AgentMessage = {
      name: `sessions/${sessionId}/agent`,
      sessionId,
      agentProfileName: runtime.profileName,
      createTime: {
        seconds: Math.floor(runtime.createdAt / 1000),
        nanos: (runtime.createdAt % 1000) * 1_000_000,
      },
    };

    callback(null, agent);
  };

  // -----------------------------------------------------------------------
  // DeleteAgent
  // -----------------------------------------------------------------------

  DeleteAgent: grpc.handleUnaryCall<{ sessionId?: string }, {}> = (
    call,
    callback,
  ) => {
    const sessionId = call.request.sessionId ?? "";
    const runtime = this.instances.get(sessionId);

    if (runtime) {
      runtime.delete();
      this.instances.delete(sessionId);
    }

    // Idempotent: missing instance = success.
    callback(null, {});
  };

  // -----------------------------------------------------------------------
  // Connect (bidirectional streaming)
  // -----------------------------------------------------------------------

  Connect: grpc.handleBidiStreamingCall<
    {
      sessionId?: string;
      frameId?: string;
      invokeId?: string;
      sequence?: number | string;
      sender?: string | number;
      payload?: string;
      text?: { content?: string } | null;
      status?: { status?: string } | null;
      screenshot?: unknown;
      echo?: unknown;
      operation?: unknown;
    },
    AgentFrame
  > = (stream) => {
    /** Tracks sequence number per invoke (turnId). */
    let currentInvokeId = "";
    let sequence = 0;

    /** Ensure sequence increments per frame within the same invoke. */
    const nextSequence = (invokeId: string): number => {
      if (invokeId !== currentInvokeId) {
        currentInvokeId = invokeId;
        sequence = 0;
      }
      return sequence++;
    };

    /** Build an AgentFrame with standard metadata. */
    const buildFrame = (
      sessionId: string,
      invokeId: string,
      sender: (typeof FrameSender)[keyof typeof FrameSender],
      payload: Partial<AgentFrame>,
    ): AgentFrame => ({
      sessionId,
      frameId: randomUUID(),
      invokeId,
      sequence: nextSequence(invokeId),
      sender,
      createTime: timestampNow(),
      ...payload,
    });

    stream.on("data", async (frame) => {
      const sessionId = frame.sessionId ?? "";
      const invokeId = frame.invokeId ?? "";

      // --- status payload: connection probe ---
      if (frame.payload === "status" || frame.status) {
        const runtime = this.instances.get(sessionId);
        const statusFrame: AgentFrame = buildFrame(
          sessionId,
          invokeId,
          FrameSender.FRAME_SENDER_SYSTEM,
          {
            status: {
              status: runtime ? runtime.getStatus() : "unknown",
            },
          },
        );
        stream.write(statusFrame);
        return;
      }

      // --- echo payload: echo back ---
      if (frame.payload === "echo" || frame.echo) {
        const echoData =
          frame.echo && typeof frame.echo === "object" && "data" in frame.echo
            ? (frame.echo as { data?: string }).data ?? ""
            : "";
        const echoFrame: AgentFrame = buildFrame(
          sessionId,
          invokeId,
          FrameSender.FRAME_SENDER_SYSTEM,
          {
            echo: { data: echoData },
          },
        );
        stream.write(echoFrame);
        return;
      }

      // --- deprecated payloads: silently ignore ---
      if (
        frame.payload === "screenshot" ||
        frame.screenshot ||
        frame.payload === "operation" ||
        frame.operation
      ) {
        return;
      }

      // --- text payload from user ---
      const senderValue =
        typeof frame.sender === "string"
          ? frame.sender
          : FrameSender.FRAME_SENDER_USER;
      if (
        (frame.payload === "text" || frame.text) &&
        senderValue === FrameSender.FRAME_SENDER_USER
      ) {
        const userText = frame.text?.content ?? "";
        const runtime = this.instances.get(sessionId);

        if (!runtime) {
          warn("runtime not found for text frame", { sessionId, invokeId });
          const warnFrame: AgentFrame = buildFrame(
            sessionId,
            invokeId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              warn: {
                message: `No agent found for session: ${sessionId}`,
              },
            },
          );
          stream.write(warnFrame);
          return;
        }

        try {
          const contentIter = runtime.processMessage(
            userText,
            invokeId,
            this.llmAdapter,
            this.providerSecret,
          );

          let blockCount = 0;
          for await (const block of contentIter) {
            blockCount++;
            if (block.type === "reasoning") {
              info("writing thinking frame", { sessionId, invokeId, length: block.reasoning.length });
              const thinkFrame: AgentFrame = buildFrame(
                sessionId,
                invokeId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  thinking: { content: block.reasoning },
                },
              );
              stream.write(thinkFrame);
            } else if (block.type === "text") {
              info("writing text frame", { sessionId, invokeId, length: block.text.length, preview: block.text.slice(0, 100) });
              const textFrame: AgentFrame = buildFrame(
                sessionId,
                invokeId,
                FrameSender.FRAME_SENDER_AGENT,
                {
                  text: { content: block.text },
                },
              );
              stream.write(textFrame);
            }
          }

          // Emit wait frame after all response frames.
          info("text processing completed", { sessionId, invokeId, blockCount });
          const waitFrame: AgentFrame = buildFrame(
            sessionId,
            invokeId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              wait: {},
            },
          );
          stream.write(waitFrame);
        } catch (err: unknown) {
          // LLM error: emit warn frame then wait frame to unblock the client.
          const message =
            err instanceof Error ? err.message : "Processing error";
          error("LLM processing failed", { sessionId, invokeId, error: message });
          const warnFrame: AgentFrame = buildFrame(
            sessionId,
            invokeId,
            FrameSender.FRAME_SENDER_SYSTEM,
            {
              warn: { message: `Processing error: ${message}` },
            },
          );
          stream.write(warnFrame);

          const waitFrame: AgentFrame = buildFrame(
            sessionId,
            invokeId,
            FrameSender.FRAME_SENDER_SYSTEM,
            { wait: {} },
          );
          stream.write(waitFrame);
        }
      }
    });

    stream.on("error", (err: Error) => {
      error("connect stream error", { error: err.message });
      // Unrecoverable: end the stream.
      stream.end();
    });

    stream.on("end", () => {
      info("connect stream ended");
    });
  };
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function timestampNow(): { seconds: number; nanos: number } {
  const ms = Date.now();
  return {
    seconds: Math.floor(ms / 1000),
    nanos: (ms % 1000) * 1_000_000,
  };
}
