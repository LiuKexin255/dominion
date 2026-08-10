import { registerResolver } from "@grpc/grpc-js/build/src/resolver";
import { Status } from "@grpc/grpc-js/build/src/constants";
import {
  statusOrFromValue,
  statusOrFromError,
} from "@grpc/grpc-js/build/src/call-interface";
import type { ResolverListener } from "@grpc/grpc-js/build/src/resolver";
import type { GrpcUri } from "@grpc/grpc-js/build/src/uri-parser";
import type {
  Endpoint,
  SubchannelAddress,
} from "@grpc/grpc-js/build/src/subchannel-address";
import type { ChannelOptions } from "@grpc/grpc-js/build/src/channel-options";
import type { StatusOr } from "@grpc/grpc-js/build/src/call-interface";

import {
  parseTarget,
  validateServiceApp,
  createResolver,
  createStatefulResolver,
  DEFAULT_REQUEST_TIMEOUT_MS,
} from "@dominion/common-js-resolver";
import type { ResolverConfig, Scheduler } from "@dominion/common-js-resolver";
import type { ResolverState } from "./grpc-types";
import { warn, info } from "@dominion/common-js-logs";

let registered = false;
let storedConfig: ResolverConfig | undefined;

/**
 * Extracts a valid HTTP/2 authority from a dominion target path.
 *
 * The path has the form `app/service:port`. We return `service:port`
 * (the segment after the last `/`) so that `grpc-js` can construct a
 * valid `http2.connect("https://service:port")` URL.
 *
 * Falls back to the full path if no `/` is found.
 */
function authorityFromPath(path: string): string {
  const lastSlash = path.lastIndexOf("/");
  if (lastSlash !== -1) {
    return path.slice(lastSlash + 1);
  }
  return path;
}

/**
 * Registers `dominion` and `dominion-stateful` URI schemes with
 * `@grpc/grpc-js` so that gRPC channels can resolve dominion service
 * targets like `dominion:///app/service:50051`.
 *
 * Safe to call multiple times — subsequent calls are no-ops.
 *
 * @param config - Optional resolver configuration (env, fetch, timers, etc.)
 */
export function registerDominionResolver(config?: ResolverConfig): void {
  if (registered) {
    return;
  }
  registered = true;
  storedConfig = config;

  class DomResolveWrapper {
    private inner: _DominionResolver;
    constructor(
      target: GrpcUri,
      listener: ResolverListener,
      channelOptions: ChannelOptions,
    ) {
      this.inner = new _DominionResolver(
        target,
        listener,
        channelOptions,
        storedConfig,
      );
    }
    updateResolution(): void {
      this.inner.updateResolution();
    }
    destroy(): void {
      this.inner.destroy();
    }
    static getDefaultAuthority(target: GrpcUri): string {
      return _DominionResolver.getDefaultAuthority(target);
    }
  }

  class DomStatefulResolveWrapper {
    private inner: _DominionStatefulResolver;
    constructor(
      target: GrpcUri,
      listener: ResolverListener,
      channelOptions: ChannelOptions,
    ) {
      this.inner = new _DominionStatefulResolver(
        target,
        listener,
        channelOptions,
        storedConfig,
      );
    }
    updateResolution(): void {
      this.inner.updateResolution();
    }
    destroy(): void {
      this.inner.destroy();
    }
    static getDefaultAuthority(target: GrpcUri): string {
      return _DominionStatefulResolver.getDefaultAuthority(target);
    }
  }

  registerResolver("dominion", DomResolveWrapper);
  registerResolver("dominion-stateful", DomStatefulResolveWrapper);
}



const DEFAULT_REFRESH_INTERVAL_MS = 30_000;

const defaultScheduler: Scheduler = {
  setInterval: (callback: () => void, ms: number): unknown => {
    return setInterval(callback, ms);
  },
  clearInterval: (handle: unknown): void => {
    clearInterval(handle as ReturnType<typeof setInterval>);
  },
};

function addressesToEndpoints(addresses: string[]): Endpoint[] {
  return addresses.map((addr): Endpoint => {
    const lastColon = addr.lastIndexOf(":");
    if (lastColon === -1) {
      return { addresses: [{ host: addr, port: 0 }] };
    }
    const host = addr.substring(0, lastColon);
    const port = parseInt(addr.substring(lastColon + 1), 10);
    return {
      addresses: [
        { host, port: Number.isNaN(port) ? 0 : port } as SubchannelAddress,
      ],
    };
  });
}

function endpointsKey(endpoints: Endpoint[]): string {
  return JSON.stringify(endpoints);
}

function parseInstanceFromPath(path: string): {
  target: string;
  instance: number;
} {
  const questionIdx = path.indexOf("?");
  if (questionIdx === -1) {
    throw new Error(
      `missing required query parameter "instance" in path ${JSON.stringify(path)}`,
    );
  }
  const targetPart = path.substring(0, questionIdx);
  const queryPart = path.substring(questionIdx + 1);
  const params = new URLSearchParams(queryPart);
  const instanceStr = params.get("instance");
  if (instanceStr === null) {
    throw new Error(
      `missing required query parameter "instance" in path ${JSON.stringify(path)}`,
    );
  }
  const instance = parseInt(instanceStr, 10);
  if (Number.isNaN(instance)) {
    throw new Error(
      `invalid instance parameter ${JSON.stringify(instanceStr)} in path ${JSON.stringify(path)}`,
    );
  }
  return { target: targetPart, instance };
}

function effectiveConfig(overrides?: ResolverConfig): ResolverConfig {
  return overrides ?? storedConfig ?? {};
}

function configEnv(cfg?: ResolverConfig): Record<string, string | undefined> {
  return (cfg?.env ?? process.env) as Record<string, string | undefined>;
}



class _DominionResolver {
  private state: ResolverState;
  private listener: ResolverListener;
  private refreshHandle: unknown | null = null;
  private scheduler: Scheduler;
  private refreshIntervalMs: number;
  private requestTimeoutMs: number;
  private targetStr: string;
  private env: Record<string, string | undefined>;
  private deployBaseUrl?: string;
  private fetchImpl?: (input: string, init?: RequestInit) => Promise<Response>;
  private lastRefreshFailed = false;

  constructor(
    target: GrpcUri,
    listener: ResolverListener,
    _channelOptions: ChannelOptions,
    config?: ResolverConfig,
  ) {
    const cfg = effectiveConfig(config);
    this.targetStr = target.path;
    const parsed = parseTarget(this.targetStr);
    this.env = configEnv(cfg);
    validateServiceApp(parsed, this.env);

    this.state = { status: "unresolved" };
    this.listener = listener;
    this.scheduler = cfg.scheduler ?? defaultScheduler;
    this.refreshIntervalMs = cfg.refreshIntervalMs ?? DEFAULT_REFRESH_INTERVAL_MS;
    this.requestTimeoutMs = cfg.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
    this.deployBaseUrl = cfg.deployBaseUrl;
    this.fetchImpl = cfg.fetch;

    setImmediate(() => this._doRefresh());

    this.refreshHandle = this.scheduler.setInterval(() => {
      if (this.state.status === "closed") return;
      this._doRefresh();
    }, this.refreshIntervalMs);
  }

  static getDefaultAuthority(target: GrpcUri): string {
    return authorityFromPath(target.path);
  }

  updateResolution(): void {
    if (this.state.status === "closed") return;
    setImmediate(() => this._doRefresh());
  }

  destroy(): void {
    if (this.state.status === "closed") return;
    this.state = { status: "closed" };
    if (this.refreshHandle !== null) {
      this.scheduler.clearInterval(this.refreshHandle);
      this.refreshHandle = null;
    }
  }

  private async _doRefresh(): Promise<void> {
    if (this.state.status === "closed") return;

    const resolver = createResolver({
      env: this.env,
      fetch: this.fetchImpl,
      deployBaseUrl: this.deployBaseUrl,
      requestTimeoutMs: this.requestTimeoutMs,
    });

    try {
      const addresses = await resolver.resolve(this.targetStr);

      // Don't publish an empty endpoint list to grpc-js. round_robin
      // destroys all subchannels on zero endpoints and enters IDLE, whose
      // exitIdle() is a no-op with zero children — the channel is
      // permanently stuck ("Waiting for LB pick", observed after a
      // prompt-service rollout). Instead:
      // - With prior valid endpoints: retain them; the stale subchannels
      //   fail naturally (TRANSIENT_FAILURE) and requestReresolution()
      //   keeps polling until new endpoints appear.
      // - Without prior endpoints: emit UNAVAILABLE so the channel enters
      //   TRANSIENT_FAILURE with backoff and retries.
      if (addresses.length === 0) {
        if (this.state.status === "ready") {
          info("service endpoints empty: retaining prior", {
            target: this.targetStr,
          });
          return;
        }
        const message = `no endpoints resolved for ${this.targetStr}`;
        this.lastRefreshFailed = true;
        warn("service endpoints empty", {
          target: this.targetStr,
        });
        const error = statusOrFromError<Endpoint[]>({
          code: Status.UNAVAILABLE,
          details: message,
        });
        setImmediate(() => this._emit(error));
        return;
      }

      const endpoints = addressesToEndpoints(addresses);
      const newKey = endpointsKey(endpoints);

      if (this.state.status === "ready" && !this.lastRefreshFailed) {
        const prevKey = endpointsKey(this.state.endpoints);
        if (newKey === prevKey) {
          info("service endpoints unchanged", {
            target: this.targetStr,
            endpoints: addresses.join(","),
          });
          return;
        }
      }

      info("service endpoints updated", {
        target: this.targetStr,
        endpoints: addresses.join(","),
      });

      this.state = {
        status: "ready",
        addresses,
        endpoints,
        lastUpdatedAt: new Date(),
      };
      this.lastRefreshFailed = false;

      const value = statusOrFromValue(endpoints);
      setImmediate(() => this._emit(value));
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : String(err);
      this.lastRefreshFailed = true;
      warn("service endpoints refresh failed", {
        target: this.targetStr,
        error: message,
      });
      const error = statusOrFromError<Endpoint[]>({
        code: Status.UNAVAILABLE,
        details: message,
      });

      setImmediate(() => this._emit(error));
    }
  }

  private _emit(statusOr: StatusOr<Endpoint[]>): void {
    if (this.state.status === "closed") return;
    try {
      this.listener(statusOr, {}, null, "");
    } catch {
    }
  }
}

class _DominionStatefulResolver {
  private state: ResolverState;
  private listener: ResolverListener;
  private refreshHandle: unknown | null = null;
  private scheduler: Scheduler;
  private refreshIntervalMs: number;
  private requestTimeoutMs: number;
  private targetStr: string;
  private instanceNum: number;
  private env: Record<string, string | undefined>;
  private deployBaseUrl?: string;
  private fetchImpl?: (input: string, init?: RequestInit) => Promise<Response>;
  private lastRefreshFailed = false;

  constructor(
    target: GrpcUri,
    listener: ResolverListener,
    _channelOptions: ChannelOptions,
    config?: ResolverConfig,
  ) {
    const cfg = effectiveConfig(config);
    const { target: targetPart, instance } = parseInstanceFromPath(target.path);
    const parsed = parseTarget(targetPart);
    this.env = configEnv(cfg);
    validateServiceApp(parsed, this.env);

    this.targetStr = targetPart;
    this.instanceNum = instance;
    this.state = { status: "unresolved" };
    this.listener = listener;
    this.scheduler = cfg.scheduler ?? defaultScheduler;
    this.refreshIntervalMs = cfg.refreshIntervalMs ?? DEFAULT_REFRESH_INTERVAL_MS;
    this.requestTimeoutMs = cfg.requestTimeoutMs ?? DEFAULT_REQUEST_TIMEOUT_MS;
    this.deployBaseUrl = cfg.deployBaseUrl;
    this.fetchImpl = cfg.fetch;

    setImmediate(() => this._doRefresh());

    this.refreshHandle = this.scheduler.setInterval(() => {
      if (this.state.status === "closed") return;
      this._doRefresh();
    }, this.refreshIntervalMs);
  }

  static getDefaultAuthority(target: GrpcUri): string {
    return authorityFromPath(target.path);
  }

  updateResolution(): void {
    if (this.state.status === "closed") return;
    setImmediate(() => this._doRefresh());
  }

  destroy(): void {
    if (this.state.status === "closed") return;
    this.state = { status: "closed" };
    if (this.refreshHandle !== null) {
      this.scheduler.clearInterval(this.refreshHandle);
      this.refreshHandle = null;
    }
  }

  private async _doRefresh(): Promise<void> {
    if (this.state.status === "closed") return;

    const resolver = createStatefulResolver({
      env: this.env,
      fetch: this.fetchImpl,
      deployBaseUrl: this.deployBaseUrl,
      requestTimeoutMs: this.requestTimeoutMs,
    });

    try {
      const addresses = await resolver.resolveInstance(
        this.targetStr,
        this.instanceNum,
      );

      // See _DominionResolver._doRefresh: an empty endpoint list must not
      // be published to grpc-js (round_robin enters IDLE with zero
      // children and no self-recovery). Retain prior endpoints, or emit
      // UNAVAILABLE when nothing was ever resolved.
      if (addresses.length === 0) {
        if (this.state.status === "ready") {
          info("service endpoints empty: retaining prior", {
            target: this.targetStr,
            instance: this.instanceNum,
          });
          return;
        }
        const message = `no endpoints resolved for ${this.targetStr}?instance=${this.instanceNum}`;
        this.lastRefreshFailed = true;
        warn("service endpoints empty", {
          target: this.targetStr,
          instance: this.instanceNum,
        });
        const error = statusOrFromError<Endpoint[]>({
          code: Status.UNAVAILABLE,
          details: message,
        });
        setImmediate(() => this._emit(error));
        return;
      }

      const endpoints = addressesToEndpoints(addresses);
      const newKey = endpointsKey(endpoints);

      if (this.state.status === "ready" && !this.lastRefreshFailed) {
        const prevKey = endpointsKey(this.state.endpoints);
        if (newKey === prevKey) {
          info("service endpoints unchanged", {
            target: this.targetStr,
            instance: this.instanceNum,
            endpoints: addresses.join(","),
          });
          return;
        }
      }

      info("service endpoints updated", {
        target: this.targetStr,
        instance: this.instanceNum,
        endpoints: addresses.join(","),
      });

      this.state = {
        status: "ready",
        addresses,
        endpoints,
        lastUpdatedAt: new Date(),
      };
      this.lastRefreshFailed = false;

      const value = statusOrFromValue(endpoints);
      setImmediate(() => this._emit(value));
    } catch (err: unknown) {
      const message = err instanceof Error ? err.message : String(err);
      this.lastRefreshFailed = true;
      warn("service endpoints refresh failed", {
        target: this.targetStr,
        error: message,
      });
      const error = statusOrFromError<Endpoint[]>({
        code: Status.UNAVAILABLE,
        details: message,
      });

      setImmediate(() => this._emit(error));
    }
  }

  private _emit(statusOr: StatusOr<Endpoint[]>): void {
    if (this.state.status === "closed") return;
    try {
      this.listener(statusOr, {}, null, "");
    } catch {
    }
  }
}



export { _DominionResolver as DominionResolver };
export { _DominionStatefulResolver as DominionStatefulResolver };
