import type { Endpoint } from "@grpc/grpc-js/build/src/subchannel-address.js";

export type ResolverState =
  | { status: "unresolved" }
  | {
      status: "ready";
      addresses: string[];
      endpoints: Endpoint[];
      lastUpdatedAt: Date;
    }
  | { status: "closed" };
