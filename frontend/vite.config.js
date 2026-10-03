import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// In development, Vite serves the React app on :5173 and forwards any
// /api request to the Go server on :8080. In production the Go server
// serves both, so no proxy is needed. OPSCOPE_API points the proxy at a
// Go server on another address, e.g. OPSCOPE_API=http://localhost:8090.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": process.env.OPSCOPE_API ?? "http://localhost:8080",
    },
  },
});
