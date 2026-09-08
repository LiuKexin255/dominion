/**
 * grpc-js Client connection adapter.
 *
 * Signature follows `specs/053-js-bootstrap-migration/contracts/bootstrap-js-api.md §6`
 * (Go reference: common/gopkg/bootstrap/grpc.go GRPCConn). Isolation
 * (contracts/bootstrap-js-api.md §7): type-only import of @grpc/grpc-js.
 */

import type { Client as GrpcClient } from "@grpc/grpc-js";
import { info } from "@dominion/common-js-logs";
import { Stage, type Component } from "./component.js";

/**
 * Client-stage component: start is a no-op (the dial happens externally)
 * and stop closes the client.
 */
export function createGrpcConnComponent(name: string, client: GrpcClient): Component {
  return {
    name,
    stage: Stage.Client,

    start: () => Promise.resolve(),

    stop: async (): Promise<void> => {
      client.close();
      info("grpc conn closed", { component: name });
    },
  };
}
