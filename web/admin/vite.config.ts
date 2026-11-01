import { fileURLToPath, URL } from "node:url";
import { resolve } from "node:path";
import vue from "@vitejs/plugin-vue";
import vueJsx from "@vitejs/plugin-vue-jsx";
import vueSetupExtend from "unplugin-vue-setup-extend-plus/vite";
import { defineConfig, loadEnv } from "vite";
import { createSvgIconsPlugin } from "vite-plugin-svg-icons";

export default defineConfig(({ mode }) => {
  // ADMIN_API_TARGET is server-side only; no backend secrets belong in VITE_*.
  const env = loadEnv(mode, process.cwd(), "");
  const target = env.ADMIN_API_TARGET || "http://127.0.0.1:18080";
  const url = new URL(target);
  if (!['http:', 'https:'].includes(url.protocol) || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname)) {
    throw new Error("ADMIN_API_TARGET must point to a local Go admin service");
  }
  return {
    base: "/",
    resolve: {
      alias: { "@": fileURLToPath(new URL("./src", import.meta.url)) }
    },
    plugins: [
      vue(), vueJsx(), vueSetupExtend({}),
      createSvgIconsPlugin({ iconDirs: [resolve(process.cwd(), "src/assets/icons")], symbolId: "icon-[dir]-[name]" })
    ],
    css: { preprocessorOptions: { scss: { additionalData: '@use "@/styles/var";' } } },
    server: {
      host: "127.0.0.1", port: 15173, strictPort: true, open: false,
      // Preserve /api: the Go routes include the prefix. Never proxy to upstream mocks.
      proxy: { "/api": { target, changeOrigin: false } }
    },
    preview: {
      proxy: { "/api": { target, changeOrigin: false } }
    },
    build: { outDir: "dist", sourcemap: false }
  };
});
