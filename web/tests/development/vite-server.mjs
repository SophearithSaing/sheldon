import { createServer } from 'vite'
import { join } from 'node:path'

const root = process.env.SHELDON_TEST_WEB_ROOT
const server = await createServer({
  root,
  configFile: join(root, 'vite.config.ts'),
  cacheDir: process.env.SHELDON_TEST_VITE_CACHE,
  mode: 'development',
})
await server.listen()
console.log('vite-ready')

async function close() {
  await server.close()
  process.exit(0)
}
process.once('SIGINT', close)
process.once('SIGTERM', close)
