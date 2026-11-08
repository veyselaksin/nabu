import { defineConfig } from "vite";

// Dev server proxies API calls to nabu-api, so no CORS setup is needed.
// Override the target with NABU_API_URL=http://host:port npm run dev.
const apiTarget = process.env.NABU_API_URL ?? "http://localhost:8080";

export default defineConfig({
  server: {
    port: 5173,
    proxy: {
      "/v1": apiTarget,
      "/healthz": apiTarget,
    },
  },
});
