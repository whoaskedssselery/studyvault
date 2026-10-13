import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, '.', 'STUDYVAULT_');
  const target = env.STUDYVAULT_API_TARGET || 'http://127.0.0.1:8080';
  return { plugins: [react()], server: { proxy: { '/backend-api': { target, changeOrigin: true, rewrite: path => path.replace(/^\/backend-api/, '') } } }, preview: { proxy: { '/backend-api': { target, changeOrigin: true, rewrite: path => path.replace(/^\/backend-api/, '') } } } };
});
