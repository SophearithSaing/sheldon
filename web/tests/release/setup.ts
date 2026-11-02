import { execFileSync, spawn } from 'node:child_process'
import { copyFile, cp, mkdir, mkdtemp, readFile, rm, symlink } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { basename, join } from 'node:path'
import { fileURLToPath } from 'node:url'

/**
 * Builds from application-owned source with no references or previous generated assets.
 * @param root Repository supplying source and installed pinned dependencies.
 * @param binary Destination for the standalone binary.
 */
async function buildRelease(root: string, binary: string): Promise<void> {
  const source = await mkdtemp(join(tmpdir(), 'sheldon-clean-build-'))
  try {
    for (const name of ['package.json', 'package-lock.json', '.npmrc', 'go.mod', 'go.sum', 'scripts', 'cmd', 'internal', 'web']) {
      await cp(join(root, name), join(source, name), {
        recursive: true,
        filter: (path): boolean => !['node_modules', 'dist', 'test-results', 'playwright-report'].includes(basename(path)),
      })
    }
    await symlink(join(root, 'node_modules'), join(source, 'node_modules'), 'dir')
    execFileSync(process.execPath, [process.env.npm_execpath!, 'run', 'build'], {
      cwd: source,
      stdio: 'inherit',
      timeout: 120000,
      env: { ...process.env, GOOS: '', GOARCH: '', GOFLAGS: '' },
    })
    await copyFile(join(source, 'bin/sheldon'), binary)
  } finally {
    await rm(source, { recursive: true, force: true })
  }
}

/**
 * Builds the release binary, starts it in an isolated environment and returns a cleanup function.
 * Exercises the actual CLI with only a binary, private storage and no desktop opener or Node on PATH.
 * @returns Cleanup function that stops the binary and removes the temporary directory.
 */
export default async function setup(): Promise<() => Promise<void>> {
  if (!['linux', 'darwin'].includes(process.platform)) {
    throw new Error('Release browser lifecycle tests currently require Linux or macOS.')
  }
  const root = fileURLToPath(new URL('../../../', import.meta.url))
  const directory = await mkdtemp(join(tmpdir(), 'sheldon-release-'))
  const binary = join(directory, 'sheldon')
  try {
    await buildRelease(root, binary)
  } catch (error) {
    await rm(directory, { recursive: true, force: true })
    throw error
  }
  const home = join(directory, 'home')
  const env = {
    HOME: home,
    PATH: directory,
    TMPDIR: directory,
    XDG_DATA_HOME: join(directory, 'data'),
    XDG_CACHE_HOME: join(directory, 'cache'),
    XDG_RUNTIME_DIR: join(directory, 'runtime'),
  }
  const stateFile =
    process.platform === 'darwin'
      ? join(home, 'Library/Application Support/sheldon/instance.lock')
      : join(directory, 'data/sheldon/instance.lock')
  await mkdir(home)
  const child = spawn(binary, [], { cwd: directory, env, stdio: ['ignore', 'pipe', 'pipe'] })
  let output = ''
  let origin = ''
  child.stdout.on('data', (data) => {
    output += data.toString()
  })
  child.stderr.on('data', (data) => {
    output += data.toString()
  })
/**
 * Stops the release binary and removes its temporary directory.
 */
  async function cleanup(): Promise<void> {
    try {
      const discovery = origin
        ? JSON.parse((await readFile(stateFile)).subarray(1).toString())
        : undefined
      if (child.pid && child.exitCode === null && child.signalCode === null) {
        const exited = new Promise<void>((resolve) => child.once('exit', () => resolve()))
        const timer = setTimeout(() => child.kill('SIGKILL'), 4000)
        child.kill('SIGTERM')
        await exited
        clearTimeout(timer)
      }
      if (!origin) return
      if (child.exitCode !== 0) throw new Error('Release CLI did not shut down gracefully.')
      if (output.includes(discovery.token)) throw new Error('Release output exposed its credential.')
      if ((await readFile(stateFile)).subarray(1).toString().trim()) {
        throw new Error('Release shutdown left discovery credentials behind.')
      }
      if (origin) {
        const response = await fetch(origin + '/api/health').catch(() => undefined)
        if (response) throw new Error('Release shutdown left the listener running.')
      }
    } finally {
      await rm(directory, { recursive: true, force: true })
    }
  }
  try {
    await new Promise<void>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error('Release CLI startup timed out.')), 10000)
      child.once('error', () => {
        clearTimeout(timer)
        reject(new Error('Release CLI startup failed.'))
      })
      child.once('exit', () => {
        clearTimeout(timer)
        reject(new Error('Release CLI exited during startup.'))
      })
      child.stdout.on('data', () => {
        if (output.includes('Sheldon: http://')) {
          clearTimeout(timer)
          resolve()
        }
      })
    })
    const state = JSON.parse((await readFile(stateFile)).subarray(1).toString())
    origin = state.url
    if (output.includes(state.token)) throw new Error('Release output exposed its credential.')
    process.env.SHELDON_RELEASE_STATE = stateFile
    process.env.SHELDON_RELEASE_BINARY = binary
    process.env.SHELDON_RELEASE_ENV = JSON.stringify(env)
    return cleanup
  } catch (error) {
    await cleanup()
    throw error
  }
}
