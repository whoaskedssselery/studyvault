import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', 'STUDYVAULT_');
  const target = env.STUDYVAULT_API_TARGET || 'http://127.0.0.1:8080';
  const proxy = { target, changeOrigin: true, cookiePathRewrite: '/backend-api/auth', rewrite: (path: string) => path.replace(/^\/backend-api/, '') };
  return { plugins: [react()], server: { proxy: { '/backend-api': proxy } }, preview: { proxy: { '/backend-api': proxy } } };
});
