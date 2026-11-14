import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";
import path from "node:path";

// Separate entry and output directory: the existing app's build is unchanged.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: { outDir: "dist-haven", rollupOptions: { input: path.resolve(__dirname, "haven.html") } },
});
