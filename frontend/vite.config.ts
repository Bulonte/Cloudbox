import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import AutoImport from 'unplugin-auto-import/vite'
import Components from 'unplugin-vue-components/vite'
import { ElementPlusResolver } from 'unplugin-vue-components/resolvers'
import { fileURLToPath, URL } from 'node:url'

// S1 工程骨架：Vite + Vue3 + TS + 按需引入 Element Plus
// 禁止全量引入 element-plus，减小打包体积
export default defineConfig({
  plugins: [
    vue(),
    AutoImport({ resolvers: [ElementPlusResolver()] }),
    Components({ resolvers: [ElementPlusResolver()] }),
  ],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
      // S3 公开分享：仅代理 /s/<code> 后端接口；必须用 ^ 正则前缀，
      // 否则 '/s' 会按 startsWith 吞掉 /src/* 模块请求导致 dev 白屏
      '^/s/.+': 'http://localhost:8080',
    },
  },
})
