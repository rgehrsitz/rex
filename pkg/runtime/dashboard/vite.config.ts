import { sveltekit } from "@sveltejs/kit/vite";
import { defineConfig } from "vite";

export default defineConfig({
  plugins: [sveltekit()],
  server: {
    proxy: {
      "/events": {
        target: "ws://localhost:8080",
        ws: true,
      },
    },
  },
  base: "/dashboard/", // Add this line
});
