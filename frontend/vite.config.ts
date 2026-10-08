import react from "@vitejs/plugin-react";
import { defineConfig, loadEnv } from "vite";

export default defineConfig(({ mode }) => {
  const acceptanceAPI = loadEnv(mode, ".", "").CLASSIFICATION_ACCEPTANCE_API;
  return {
    plugins: [react()],
    server: acceptanceAPI
      ? {
          proxy: {
            "/api": acceptanceAPI,
            "/v1": acceptanceAPI,
            "/e2e": acceptanceAPI,
          },
        }
      : undefined,
  };
});
