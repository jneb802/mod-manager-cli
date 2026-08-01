/// <reference types="vite/client" />

declare global {
  interface Window {
    go?: {
      app?: {
        Service?: Record<string, (...args: unknown[]) => Promise<unknown>>;
      };
    };
  }
}

export {};
