import { defineConfig } from "vitest/config";
import react from "@vitejs/plugin-react";
export default defineConfig({
    plugins: [react()],
    test: {
        exclude: ["e2e/**", "node_modules/**"],
        environment: "jsdom",
    },
    build: {
        outDir: "../internal/web/static/dist",
        emptyOutDir: true,
        assetsDir: "assets",
    },
    server: {
        port: 5173,
        proxy: { "/api": "http://127.0.0.1:8787" },
    },
});
