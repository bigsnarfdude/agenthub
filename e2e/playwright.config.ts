import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./tests",
  timeout: 30_000,
  retries: 0,
  use: {
    baseURL: process.env.BASE_URL || "http://localhost:8080",
  },
  // Run tests sequentially — later tests depend on earlier ones (agents, channels, etc.)
  workers: 1,
});
