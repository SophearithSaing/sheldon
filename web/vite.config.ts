import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'
import { dirname } from 'node:path'
import { fileURLToPath } from 'node:url'
import { backendProxy, loopbackOrigin, protectedDevelopmentServer } from './development/server.ts'

export default defineConfig(({ command }) => {
  if (command !== 'serve') return { plugins: [vue()] }
  const frontend = loopbackOrigin(process.env.SHELDON_DEV_ORIGIN ?? 'http://127.0.0.1:5173')
  const backend = process.env.SHELDON_BACKEND_URL ?? 'http://127.0.0.1:7331'
  return {
    plugins: [protectedDevelopmentServer(), vue()],
    server: {
      host: frontend.hostname.replace(/^\[|\]$/g, ''),
      port: Number(frontend.port),
      strictPort: true,
      cors: false,
      allowedHosts: [frontend.hostname],
      fs: {
        allow: [
          fileURLToPath(new URL('.', import.meta.url)),
          dirname(dirname(fileURLToPath(import.meta.resolve('vue/package.json')))),
        ],
      },
      proxy: {
        '^/api(?:/|$)': backendProxy(backend),
        '^/instance-proof(?:\\?|$)': backendProxy(backend),
      },
    },
  }
})
