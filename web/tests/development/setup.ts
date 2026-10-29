import { spawn, execFile } from 'node:child_process'
import type { ChildProcess } from 'node:child_process'
import { promisify } from 'node:util'
import { cp, mkdir, mkdtemp, readFile, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { createHmac, randomBytes } from 'node:crypto'

const root = fileURLToPath(new URL('../../../', import.meta.url))
const frontend = 'http://127.0.0.1:5175'
const backend = 'http://127.0.0.1:7332'

async function stop(child: ChildProcess) {
  if (!child.pid || child.exitCode !== null || child.signalCode !== null) return
  child.kill('SIGTERM')
  const timer = setTimeout(() => child.kill('SIGKILL'), 4000)
  await new Promise<void>((resolve) => child.once('exit', () => resolve()))
  clearTimeout(timer)
}

async function start(
  command: string,
  args: string[],
  env: NodeJS.ProcessEnv,
  ready: string,
  log: string,
): Promise<ChildProcess> {
  const child = spawn(command, args, { cwd: root, env, stdio: ['ignore', 'pipe', 'pipe'] })
  child.stdout!.on('data', (data) => {
    void writeFile(log, data, { flag: 'a', mode: 0o600 }).catch(() => {})
  })
  child.stderr!.on('data', (data) => {
    void writeFile(log, data, { flag: 'a', mode: 0o600 }).catch(() => {})
  })
  try {
    await new Promise<void>((resolve, reject) => {
      const timeout = setTimeout(
        () => reject(new Error('Development test server did not start.')),
        15000,
      )
      child.once('error', () => {
        clearTimeout(timeout)
        reject(new Error('Development test server failed to start.'))
      })
      child.once('exit', () => {
        clearTimeout(timeout)
        reject(new Error('Development test server exited during startup.'))
      })
      child.stdout!.on('data', (data) => {
        if (data.toString().includes(ready)) {
          clearTimeout(timeout)
          resolve()
        }
      })
    })
    return child
  } catch (error) {
    await stop(child)
    throw error
  }
}

// Copy the frontend before editing it for the HMR check. Never modify the user's sources.
export default async function setup() {
  const directory = await mkdtemp(join(tmpdir(), 'sheldon-development-'))
  const webRoot = join(directory, 'web')
  const stateFile = join(directory, 'backend.json')
  const logFile = join(directory, 'servers.log')
  const children: ChildProcess[] = []
  async function cleanup() {
    await Promise.all(children.map(stop))
    await rm(directory, { recursive: true, force: true })
  }
  try {
    await mkdir(join(directory, 'home'))
    await cp(join(root, 'web'), webRoot, {
      recursive: true,
      filter: (path) =>
        !/(?:^|[/\\])(?:node_modules|dist|test-results|playwright-report)(?:[/\\]|$)/.test(path),
    })
    await symlink(
      join(root, 'node_modules'),
      join(webRoot, 'node_modules'),
      process.platform === 'win32' ? 'junction' : 'dir',
    )
    await writeFile(join(webRoot, 'instance.lock'), 'private fixture value', { mode: 0o600 })
    const binary = join(directory, process.platform === 'win32' ? 'backend.exe' : 'backend')
    await promisify(execFile)(
      'go',
      ['build', '-o', binary, './internal/httpapi/testdata/devserver'],
      { cwd: root, timeout: 60000 },
    )
    const env = {
      ...process.env,
      HOME: join(directory, 'home'),
      SHELDON_DEV_ORIGIN: frontend,
      SHELDON_DEV_LISTEN: '127.0.0.1:7332',
      SHELDON_BACKEND_URL: backend,
      SHELDON_TEST_STATE: stateFile,
      SHELDON_TEST_WEB_ROOT: webRoot,
      SHELDON_TEST_VITE_CACHE: join(directory, 'vite-cache'),
    }
    children.push(await start(binary, [], env, 'backend-ready', logFile))
    children.push(
      await start(
        process.execPath,
        [join(root, 'web/tests/development/vite-server.mjs')],
        env,
        'vite-ready',
        logFile,
      ),
    )
    const state: { token: string } = JSON.parse(await readFile(stateFile, 'utf8'))
    const challenge = randomBytes(32).toString('base64url')
    const response = await fetch(frontend + '/instance-proof', {
      headers: { 'X-Sheldon-Challenge': challenge },
      redirect: 'error',
    })
    const proof = createHmac('sha256', state.token)
      .update(`sheldon-instance-v1\n${backend}\n${challenge}`)
      .digest('base64url')
    if (!response.ok || (await response.json()).proof !== proof)
      throw new Error('Development proxy verification failed.')
    process.env.SHELDON_TEST_STATE = stateFile
    process.env.SHELDON_TEST_WEB_ROOT = webRoot
    process.env.SHELDON_TEST_SERVER_LOG = logFile
    return cleanup
  } catch (error) {
    await cleanup()
    throw error
  }
}
