import { spawnSync } from 'node:child_process'
import { cp, mkdir, rm } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'

const root = fileURLToPath(new URL('../', import.meta.url))

// Stop on any failed step; never compile a release with a stale frontend bundle.
function run(command, args) {
  const result = spawnSync(command, args, { cwd: root, stdio: 'inherit' })
  if (result.error) throw result.error
  if (result.status !== 0) process.exit(result.status ?? 1)
}

if (!process.env.npm_execpath) throw new Error('Run this script with npm run build.')
run(process.execPath, [process.env.npm_execpath, 'run', 'build:web'])
const assets = new URL('../internal/ui/dist/', import.meta.url)
await rm(assets, { recursive: true, force: true })
await cp(new URL('../web/dist/', import.meta.url), assets, { recursive: true })
await mkdir(new URL('../bin/', import.meta.url), { recursive: true })
const target = process.env.GOOS || process.platform
run('go', [
  'build',
  '-tags=release',
  '-trimpath',
  '-o',
  `bin/sheldon${target === 'windows' || target === 'win32' ? '.exe' : ''}`,
  './cmd/sheldon',
])
