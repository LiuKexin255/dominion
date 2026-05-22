// Type declarations for Wails runtime injected globals.
// The wailsjs/ directory is generated at build-time by `wails dev` / `wails build`.
// Until then, these declarations let TypeScript compile without errors.

export {}; // make this a module

declare global {
  interface Window {
    go: {
      app: {
        App: {
          ListSessions: () => Promise<Session[]>;
          CreateSession: (sessionType: string) => Promise<Session>;
          ConnectSession: (session: Session) => Promise<void>;
          DeleteSession: (name: string) => Promise<void>;
          ClearWindow: () => Promise<void>;
          StartCapture: () => Promise<void>;
          StopCapture: () => Promise<void>;
          TakeScreenshot: () => Promise<ScreenshotResult>;
          GetStatus: () => Promise<AgentStatus>;
          FlushInitErrors: () => Promise<void>;
          EnumerateWindows: () => Promise<WindowInfo[]>;
          BindWindow: (hwnd: number) => Promise<void>;
          Disconnect: () => Promise<void>;
        };
      };
    };
    runtime: {
      EventsOn: (event: string, ...callbacks: Array<(...args: any[]) => void>) => void;
      EventsEmit: (event: string, ...data: any[]) => void;
      EventsOff: (event: string) => void;
    };
  }
}

interface WindowInfo {
  HWND: number;
  Title: string;
  ClassName: string;
  ProcessID: number;
  Rect: { Left: number; Top: number; Right: number; Bottom: number };
}

interface WindowRef {
  hwnd: number;
  title: string;
}

interface WindowDetail extends WindowRef {
  className: string;
  processId: number;
  rect: { left: number; top: number; right: number; bottom: number };
}

interface Session {
  name: string;
  type: string;
  status: string;
  runtimeId: string;
  agentConnectUrl: string;
  createTime: string;
  updateTime: string;
  reconnectGeneration: string;
  lastError: string;
}

interface LogEntry {
  timestamp: string;
  level: string;
  module: string;
  message: string;
  fields: Record<string, string>;
}

interface ScreenshotResult {
  imageURL: string;
  mimeType: string;
  snapshotID: string;
  captureTime: string;
  sessionName: string;
  runtimeID: string;
  error: string;
}

interface AgentStatus {
  state: string;
  sessionId: string;
  boundWindow: WindowDetail | null;
  mediaSegCount: number;
  lastError: string;
  ffmpegRunning: boolean;
  helperRunning: boolean;
  connectedAt: string;
  sessionName: string;
  sessionType: string;
  runtimeId: string;
  streamingStartedAt: string;
  sessionServiceState: string;
  sessionServiceError: string;
}
