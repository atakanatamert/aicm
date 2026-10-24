import { writeFileSync } from "node:fs"
import path from "node:path"
import tailwindcss from "@tailwindcss/vite"
import react from "@vitejs/plugin-react"
import { defineConfig } from "vite"

// go:embed needs web/dist to hold a file before the first build, so git
// tracks web/dist/.keep. Vite empties dist on every build; put it back.
const keepDistPlaceholder = {
  name: "keep-dist-placeholder",
  closeBundle() {
    writeFileSync(path.resolve(__dirname, "dist/.keep"), "")
  },
}

// https://vite.dev/config/
export default defineConfig({
  plugins: [react(), tailwindcss(), keepDistPlaceholder],
  resolve: {
    alias: {
      "@": path.resolve(__dirname, "./src"),
    },
  },
  server: {
    proxy: {
      "/api": "http://localhost:8080",
      "/healthz": "http://localhost:8080",
    },
  },
  build: {
    rollupOptions: {
      output: {
        manualChunks: {
          react: ["react", "react-dom", "react-router"],
          query: ["@tanstack/react-query", "openapi-fetch", "openapi-react-query"],
          radix: ["radix-ui"],
          hairline: ["@lucasmarkes/hairline"],
          motion: ["motion"],
        },
      },
    },
  },
})
