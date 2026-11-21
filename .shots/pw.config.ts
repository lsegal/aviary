import { defineConfig, devices } from "@playwright/test";
export default defineConfig({ testDir: ".", reporter: "list", use: { baseURL: process.env.BASE, ...devices["Desktop Chrome"] } });
