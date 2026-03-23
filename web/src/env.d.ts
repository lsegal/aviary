// Environment types for import.meta.env used in the web app
// This file ensures TypeScript understands import.meta.env.DEV and other fields.

declare global {
  interface ImportMetaEnv {
    readonly DEV?: boolean;
    readonly PROD?: boolean;
    readonly [key: string]: any;
  }

  interface ImportMeta {
    readonly env: ImportMetaEnv;
  }
}

export {};
