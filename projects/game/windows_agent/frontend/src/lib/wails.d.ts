// Type declarations for Wails runtime injected globals.
// The wailsjs/ directory is generated at build-time by `wails dev` / `wails build`.
// Until then, these declarations let TypeScript compile without errors.

export {}; // make this a module

declare global {
  interface Window {
    go: {
      main: {
        App: {
          Connect: (url: string) => Promise<void>;
          Disconnect: () => Promise<void>;
          EnumerateWindows: () => Promise<WindowInfo[]>;
          BindWindow: (hwnd: number) => Promise<void>;
          GetStatus: () => Promise<AgentStatus>;
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

interface AgentStatus {
  state: string;
  sessionId: string;
  boundWindow: WindowRef | null;
  mediaSegCount: number;
  lastError: string;
  ffmpegRunning: boolean;
  helperRunning: boolean;
  connectedAt: string;
}
