import { spawn, execFile, type ChildProcess } from 'node:child_process'
import { promisify } from 'node:util'
import { constants } from 'node:fs'
import { access, copyFile, cp, mkdir, mkdtemp, readFile, realpath, rename, rm, symlink, writeFile } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { delimiter, isAbsolute, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { expect } from '@playwright/test'
import { createServer } from 'node:net'

const execute = promisify(execFile)
const repository = fileURLToPath(new URL('../../../', import.meta.url))

export interface Discovery { url: string; token: string }
export interface GitAudit { Operation: string; Directory: string; Safe: boolean; PID: number }
export interface NativeAudit { args?: string[]; directory?: string; pid?: number; restricted?: boolean; method?: string; path?: string; body?: string }
export interface OfflineCLI {
  directory: string
  cache: string
  stateFile: string
  upstream: string
  mergeBase: string
  baseTip: string
  firstHead: string
  secondHead: string
  start(url: string, hangFetch?: boolean): Promise<string>
  open(url: string): Promise<string>
  stop(): Promise<void>
  crash(): Promise<void>
  close(): Promise<void>
  discovery(): Promise<Discovery>
  git(dir: string, input: string, ...args: string[]): Promise<string>
  head(number: number, text: string): Promise<string>
  audit(): Promise<GitAudit[]>
  /**
   * Reads synthetic native protocol activity without transport passwords.
   * @returns Completed native fixture audit records.
   */
  nativeAudit(): Promise<NativeAudit[]>
  /**
   * Releases only a numbered synthetic native input; no real model is called.
   * @param input Native fixture prompt ordinal.
   * @param outcome Explicit synthetic terminal outcome.
   */
  answer(input: number, outcome: 'succeeded' | 'failed'): Promise<void>
}

/**
 * Chooses an unused explicit loopback port for a disposable development frontend.
 * @returns Canonical frontend origin.
 */
async function developmentOrigin(): Promise<string> {
  const listener = createServer()
  return new Promise<string>((resolve, reject): void => {
    listener.once('error', reject)
    listener.listen(0, '127.0.0.1', (): void => {
      const address = listener.address()
      if (!address || typeof address === 'string') { listener.close(); reject(new Error('Development frontend port unavailable.')); return }
      listener.close((): void => resolve(`http://127.0.0.1:${address.port}`))
    })
  })
}

/**
 * Finds a native tool on the runner's existing absolute PATH.
 * @param name Executable name.
 * @returns Resolved absolute path.
 */
async function executable(name: string): Promise<string> {
  for (const directory of (process.env.PATH ?? '').split(delimiter)) {
    if (!isAbsolute(directory)) continue
    const path = join(directory, name)
    try { await access(path, constants.X_OK); return await realpath(path) } catch {}
  }
  throw new Error(`Native ${name} is required for offline CLI integration.`)
}

/**
 * Reads only complete private JSON-line fixture events.
 * @param path Fixture journal path.
 * @returns Completed decoded records.
 */
async function events<T>(path: string): Promise<T[]> {
  const text = await readFile(path, 'utf8').catch((): string => '')
  return text.split('\n').slice(0, -1).filter(Boolean).map((line: string): T => JSON.parse(line) as T)
}

/**
 * Creates native offline objects and tools for an unmodified standalone release binary.
 * @param options Optional real development CLI/Vite and synthetic native-answer tools.
 * @returns Isolated CLI controller with deterministic native PR source and explicit cleanup.
 */
export async function offlineCLI(options: { development?: boolean; questions?: boolean; nativePersistence?: boolean } = {}): Promise<OfflineCLI> {
  const realGit = await executable('git')
  const directory = await mkdtemp(join(tmpdir(), 'sheldon-source-cli-'))
  const tools = join(directory, 'tools')
  const home = join(directory, 'home')
  const cwd = join(directory, 'working directory 日本語')
  const upstream = join(directory, 'upstream.git')
  const metadata = join(directory, 'metadata.json')
  const launches = join(directory, 'launches.jsonl')
  const auditPath = join(directory, 'git-audit.jsonl')
  const nativeAuditPath = join(directory, 'native-audit.jsonl')
  const nativeControl = join(directory, 'native-control.json')
  const frontend = options.development ? await developmentOrigin() : ''
  const webRoot = join(directory, 'web')
  const data = process.platform === 'darwin' ? join(home, 'Library/Application Support/sheldon') : join(directory, 'data/sheldon')
  const cache = process.platform === 'darwin' ? join(home, 'Library/Caches/sheldon') : join(directory, 'cache/sheldon')
  const stateFile = join(data, 'instance.lock')
  const binary = options.development ? join(directory, 'sheldon-development') : process.env.SHELDON_RELEASE_BINARY!
  const nativeEnv = {
    PATH: process.env.PATH, HOME: home, TMPDIR: directory,
    GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null', GIT_CONFIG_NOSYSTEM: '1',
    GIT_AUTHOR_NAME: 'Offline fixture', GIT_AUTHOR_EMAIL: 'fixture@example.invalid',
    GIT_COMMITTER_NAME: 'Offline fixture', GIT_COMMITTER_EMAIL: 'fixture@example.invalid',
  }
  const env = {
    HOME: home, PATH: tools, TMPDIR: directory,
    XDG_DATA_HOME: join(directory, 'data'), XDG_CACHE_HOME: join(directory, 'cache'), XDG_RUNTIME_DIR: join(directory, 'runtime'),
    GIT_CONFIG_GLOBAL: '/dev/null', GIT_CONFIG_SYSTEM: '/dev/null', GIT_CONFIG_NOSYSTEM: '1',
    GIT_SSH_COMMAND: 'preserved-fixture-ssh-route',
    SHELDON_GIT_REAL: realGit, SHELDON_GIT_UPSTREAM: upstream, SHELDON_GIT_AUDIT: auditPath, SHELDON_GIT_MODE: '',
    SHELDON_SOURCE_METADATA: metadata, SHELDON_SOURCE_LAUNCHES: launches, SHELDON_SOURCE_GH_AUDIT: join(directory, 'gh-audit.jsonl'),
    SHELDON_NATIVE_MODE: 'answer-browser', SHELDON_NATIVE_AUDIT: nativeAuditPath, SHELDON_NATIVE_CONTROL: nativeControl,
    SHELDON_NATIVE_STATE: options.nativePersistence ? join(directory, 'synthetic-native-state') : '',
    ...(frontend ? { SHELDON_DEV_ORIGIN: frontend, SHELDON_DEV_LISTEN: '127.0.0.1:0' } : {}),
  }
  let child: ChildProcess | undefined
  let vite: ChildProcess | undefined
  let output = ''
  let current: Discovery | undefined
  const payload: { prs: Record<string, unknown>; fallback: Record<string, boolean> } = { prs: {}, fallback: { '32': true } }

  /**
   * Runs isolated native Git setup without user configuration or shell interpretation.
   * @param dir Native fixture repository or worktree.
   * @param input Literal stdin bytes.
   * @param args Fixed native Git arguments.
   * @returns Trimmed native stdout.
   */
  async function git(dir: string, input: string, ...args: string[]): Promise<string> {
    return new Promise<string>((resolve, reject): void => {
      const command = spawn(realGit, ['-c', 'core.hooksPath=/dev/null', '-c', 'commit.gpgsign=false', ...args], { cwd: dir, env: nativeEnv, stdio: ['pipe', 'pipe', 'pipe'] })
      let stdout = ''
      command.stdout.on('data', (chunk: Buffer): void => { stdout += chunk.toString() })
      command.stderr.resume()
      command.once('error', (): void => reject(new Error('Native Git fixture setup failed.')))
      command.once('exit', (code: number | null): void => { if (code === 0) resolve(stdout.trim()); else reject(new Error('Native Git fixture command failed.')) })
      command.stdin.on('error', (failure: NodeJS.ErrnoException): void => { if (failure.code !== 'EPIPE' || input) reject(new Error('Native Git fixture stdin failed.')) })
      command.stdin.end(input)
    })
  }

  /**
   * Builds literal top-level files as a native Git tree, without a checkout.
   * @param files Fixture file contents.
   * @returns Immutable native tree identity.
   */
  async function tree(files: Record<string, string>): Promise<string> {
    let manifest = ''
    for (const name of Object.keys(files).sort()) manifest += `100644 blob ${await git(upstream, files[name]!, 'hash-object', '-w', '--stdin')}\t${name}\0`
    return git(upstream, manifest, 'mktree', '-z')
  }

  /** Atomically replaces metadata used only by the synthetic native gh fixture. */
  async function saveMetadata(): Promise<void> {
    await writeFile(metadata + '.next', JSON.stringify(payload), { mode: 0o600 })
    await rename(metadata + '.next', metadata)
  }

  /**
   * Reads the real CLI's private instance record.
   * @returns Current origin and bearer, kept out of console output.
   */
  async function discovery(): Promise<Discovery> { return JSON.parse((await readFile(stateFile)).subarray(1).toString()) as Discovery }

  /**
   * Reads a privately captured browser launch after the native opener exits.
   * @param before Number of previous launches.
   * @returns Newly captured token-bearing launch URL.
   */
  async function launch(before: number): Promise<string> {
    await expect.poll(async (): Promise<number> => (await events<{ url: string }>(launches)).length).toBe(before + 1)
    return (await events<{ url: string }>(launches))[before]!.url
  }

  /**
   * Starts the real CLI as writer with a first URL, optionally hanging its test-only fetch tool.
   * @param url Initial PR URL to register.
   * @param hangFetch Whether the offline Git fixture should await cancellation.
   * @returns Captured browser launch URL.
   */
  async function start(url: string, hangFetch = false): Promise<string> {
    const before = (await events<{ url: string }>(launches)).length
    output = ''
    child = spawn(binary, ['open', url], { cwd, env: { ...env, SHELDON_GIT_MODE: hangFetch ? 'hang' : '' }, stdio: ['ignore', 'pipe', 'pipe'] })
    const started = child
    started.stderr!.on('data', (chunk: Buffer): void => { output += chunk.toString() })
    await new Promise<void>((resolve, reject): void => {
      const timer = setTimeout((): void => reject(new Error('Offline source CLI startup timed out.')), 10000)
      started.once('error', (): void => { clearTimeout(timer); reject(new Error('Offline source CLI failed to start.')) })
      started.once('exit', (): void => { clearTimeout(timer); reject(new Error('Offline source CLI exited during startup.')) })
      started.stdout!.on('data', (chunk: Buffer): void => {
        output += chunk.toString()
        if (output.includes('Sheldon: http://')) { clearTimeout(timer); resolve() }
      })
    })
    current = await discovery()
    if (output.includes(current.token)) throw new Error('CLI output exposed its launch credential.')
    if (frontend) {
      await expect.poll((): boolean => output.includes('The backend remains running.')).toBe(true)
      vite = spawn(process.execPath, [join(repository, 'web/tests/development/vite-server.mjs')], {
        cwd: directory, stdio: ['ignore', 'pipe', 'pipe'],
        env: { ...process.env, SHELDON_DEV_ORIGIN: frontend, SHELDON_BACKEND_URL: current.url, SHELDON_TEST_WEB_ROOT: webRoot, SHELDON_TEST_VITE_CACHE: join(directory, 'vite-cache') },
      })
      const server = vite
      server.stderr!.resume()
      await new Promise<void>((resolve, reject): void => {
        const timer = setTimeout((): void => reject(new Error('Native question development frontend timed out.')), 15000)
        server.once('error', (): void => { clearTimeout(timer); reject(new Error('Development frontend failed to start.')) })
        server.once('exit', (): void => { clearTimeout(timer); reject(new Error('Development frontend exited during startup.')) })
        server.stdout!.on('data', (chunk: Buffer): void => { if (chunk.toString().includes('vite-ready')) { clearTimeout(timer); resolve() } })
      })
      const forwarded = await execute(binary, ['open', url], { cwd, env, timeout: 10000 })
      if ((forwarded.stdout + forwarded.stderr).includes(current.token)) throw new Error('Development forwarding exposed its credential.')
    }
    return launch(before)
  }

  /**
   * Exercises verified second-instance forwarding through the real executable.
   * @param url PR URL or normalized alias.
   * @returns Captured browser launch URL from the forwarding CLI.
   */
  async function open(url: string): Promise<string> {
    const before = (await events<{ url: string }>(launches)).length
    const result = await execute(binary, ['open', url], { cwd, env, timeout: 10000 })
    if (current && (result.stdout + result.stderr).includes(current.token)) throw new Error('Forwarded CLI output exposed its credential.')
    return launch(before)
  }

  /**
   * Stops the actual writer, checking orderly cleanup or intentionally bypassing shutdown.
   * @param abrupt Whether to SIGKILL the writer and clean up only test-owned native children.
   */
  async function stop(abrupt = false): Promise<void> {
    if (vite?.pid && vite.exitCode === null && vite.signalCode === null) {
      const server = vite
      const done = new Promise<void>((resolve): void => { server.once('exit', (): void => resolve()) })
      const timer = setTimeout((): void => { server.kill('SIGKILL') }, 5000)
      server.kill('SIGTERM')
      await done
      clearTimeout(timer)
    }
    vite = undefined
    const active = child
    if (!active) return
    if (active.pid && active.exitCode === null && active.signalCode === null) {
      const done = new Promise<void>((resolve): void => { active.once('exit', (): void => resolve()) })
      const timer = setTimeout((): void => { active.kill('SIGKILL') }, 5000)
      active.kill(abrupt ? 'SIGKILL' : 'SIGTERM')
      await done
      clearTimeout(timer)
    }
    child = undefined
    if (abrupt) {
      for (const entry of await events<NativeAudit>(nativeAuditPath)) {
        if (!entry.pid || !entry.args?.includes('serve')) continue
        try { process.kill(-entry.pid, 'SIGKILL') } catch (failure: unknown) { if ((failure as NodeJS.ErrnoException).code !== 'ESRCH') throw failure }
      }
      if (active.signalCode !== 'SIGKILL') throw new Error('Offline CLI crash did not bypass graceful shutdown.')
      return
    }
    if (active.exitCode !== 0) throw new Error('Offline source CLI did not stop gracefully.')
    if (current && output.includes(current.token)) throw new Error('CLI output exposed its credential.')
    if ((await readFile(stateFile)).subarray(1).toString().trim()) throw new Error('Shutdown retained instance credentials.')
    if (current && await fetch(current.url + '/api/health', { signal: AbortSignal.timeout(1000) }).catch((): undefined => undefined)) throw new Error('Shutdown retained a listener.')
  }

  /** Kills only this fixture's writer without allowing application shutdown saves. */
  async function crash(): Promise<void> { await stop(true) }

  /** Stops owned children and removes only this fixture's temporary directory. */
  async function close(): Promise<void> {
    try { await stop() } finally { await rm(directory, { recursive: true, force: true }) }
  }

  try {
    for (const name of [tools, home, cwd]) await mkdir(name, { mode: 0o700 })
    await execute('go', ['build', '-o', join(tools, 'git'), './internal/git/testdata/git'], { cwd: repository, timeout: 60000 })
    await execute('go', ['build', '-o', join(tools, 'gh'), './internal/httpapi/testdata/sourcecli'], { cwd: repository, timeout: 60000 })
    if (options.questions) await execute('go', ['build', '-o', join(tools, 'opencode'), './internal/opencode/testdata/opencode'], { cwd: repository, timeout: 60000 })
    if (frontend) {
      await execute('go', ['build', '-o', binary, './cmd/sheldon'], { cwd: repository, timeout: 60000 })
      await cp(join(repository, 'web'), webRoot, { recursive: true, filter: (path: string): boolean => !/(?:^|[/\\])(?:node_modules|dist|test-results|playwright-report)(?:[/\\]|$)/.test(path) })
      await symlink(join(repository, 'node_modules'), join(webRoot, 'node_modules'), 'dir')
    }
    await copyFile(join(tools, 'gh'), join(tools, process.platform === 'darwin' ? 'open' : 'xdg-open'))
    await git(directory, '', 'init', '--bare', upstream)
    const baseTree = await tree({ 'shared.txt': 'before\n', 'remove.txt': 'delete me\n' })
    const mergeBase = await git(upstream, 'fork point\n', 'commit-tree', baseTree)
    const baseTip = await git(upstream, 'base branch advanced\n', 'commit-tree', await tree({ 'shared.txt': 'before\n', 'remove.txt': 'delete me\n', 'base-only.txt': 'not a PR change\n' }), '-p', mergeBase)
    const firstText = options.questions ? 'after PR31 <script>literal</script> ' + 'long literal source '.repeat(60) + '\n' + Array.from({ length: 80 }, (_, index: number): string => `fixture source line ${index + 2}`).join('\n') + '\n' : 'after PR31 <script>literal</script>\n'
    const firstHead = await git(upstream, 'first PR\n', 'commit-tree', await tree({ 'shared.txt': firstText }), '-p', mergeBase)
    const secondHead = await git(upstream, 'second PR\n', 'commit-tree', await tree({ 'shared.txt': 'before\n', 'remove.txt': 'delete me\n', 'notes.txt': 'only PR32\n' }), '-p', mergeBase)
    const wrongBase = await git(upstream, 'unrelated default\n', 'commit-tree', await tree({ 'wrong.txt': 'must not be selected automatically\n' }))
    for (const [ref, sha] of Object.entries({ 'refs/heads/release/target': baseTip, 'refs/heads/main': wrongBase, 'refs/pull/31/head': firstHead, 'refs/pull/32/head': secondHead })) await git(upstream, '', 'update-ref', ref, sha)
    for (const [number, sha] of [[31, firstHead], [32, secondHead]] as const) payload.prs[String(number)] = {
      number, html_url: `https://github.com/fixture/repo/pull/${number}`, title: `Offline CLI PR ${number}`, user: { login: 'native-fixture-author' },
      base: { ref: 'release/target', sha: baseTip, repo: { full_name: 'fixture/repo' } }, head: { sha, repo: { clone_url: 'file:///must-never-be-used' } },
    }
    await saveMetadata()
    /**
     * Force-pushes native PR source and updates only the matching synthetic metadata payload.
     * @param number Fixture PR number.
     * @param text Literal replacement source text.
     * @returns New native head commit.
     */
    async function head(number: number, text: string): Promise<string> {
      const sha = await git(upstream, 'force-pushed fixture\n', 'commit-tree', await tree({ 'shared.txt': text }), '-p', mergeBase)
      await git(upstream, '', 'update-ref', `refs/pull/${number}/head`, sha)
      const value = payload.prs[String(number)] as { head: { sha: string } }
      value.head.sha = sha
      await saveMetadata()
      return sha
    }
    /**
     * Reads completed native Git audit events without exposing command diagnostics.
     * @returns Fixture command audit records.
     */
    async function audit(): Promise<GitAudit[]> { return events<GitAudit>(auditPath) }
    /** Reads completed synthetic native records without credentials.
     * @returns Native protocol audit records.
     */
    async function nativeAudit(): Promise<NativeAudit[]> { return events<NativeAudit>(nativeAuditPath) }
    /**
     * Atomically releases one synthetic answer without issuing any real inference request.
     * @param input Native prompt ordinal to release.
     * @param outcome Synthetic terminal outcome.
     */
    async function answer(input: number, outcome: 'succeeded' | 'failed'): Promise<void> {
      await writeFile(nativeControl + '.next', JSON.stringify({ input, outcome }), { mode: 0o600 })
      await rename(nativeControl + '.next', nativeControl)
    }
    return { directory, cache, stateFile, upstream, mergeBase, baseTip, firstHead, secondHead, start, open, stop, crash, close, discovery, git, head, audit, nativeAudit, answer }
  } catch (failure: unknown) {
    await close()
    throw failure
  }
}
