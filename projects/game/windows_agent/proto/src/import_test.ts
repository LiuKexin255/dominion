import type { GameWebSocketEnvelope } from "../projects/game/gateway/gateway";
import {
  GameClientRole,
  GameControlOperationKind,
  GameMouseButton,
} from "../projects/game/gateway/gateway";

export const sampleEnvelope: GameWebSocketEnvelope = {
  sessionId: "sessions/import-test",
  messageId: "message-import-test",
  payload: {
    oneofKind: "controlRequest",
    controlRequest: {
      operationId: "operation-import-test",
      kind: GameControlOperationKind.GAME_CONTROL_OPERATION_KIND_MOUSE_CLICK,
      flashSnapshot: false,
      mouse: {
        button: GameMouseButton.GAME_MOUSE_BUTTON_LEFT,
        x: 1,
        y: 2,
        fromX: 0,
        fromY: 0,
        toX: 0,
        toY: 0,
        durationMs: 0,
      },
    },
  },
};

export const agentRole = GameClientRole.GAME_CLIENT_ROLE_WINDOWS_AGENT;
