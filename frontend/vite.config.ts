import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

export default defineConfig({
  plugins: [react()],
  // The Go binary embeds this directory and serves it at /.
  build: {
    outDir: "../backend/dist",
    emptyOutDir: true,
  },
  server: {
    // During `npm run dev`, proxy API calls to the Go server on :8080.
    proxy: {
      "/api": "http://localhost:8080",
    },
  },
});
