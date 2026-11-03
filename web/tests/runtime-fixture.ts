import { expect, type Page } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import type { RegisteredPR } from '../src/runtime/client'
import type { SourceResponse, SourceFile, SourceLine } from '../src/runtime/source'

/**
 * Hashes explicit fixture identities, never a real source claim.
 * @param value Fixture identity.
 * @returns Full digest.
 */
export function digest(value: string): string { return createHash('sha256').update(value).digest('hex') }

/**
 * Reads only the isolated development fixture credential.
 * @returns Fixture backend credential.
 */
export async function token(): Promise<string> { return JSON.parse(await readFile(process.env.SHELDON_TEST_STATE!, 'utf8')).token }

/**
 * Registers an offline browser fixture and connects its routed page.
 * @param page Browser page.
 * @param number Distinct fixture PR number.
 * @returns Registered fixture identity.
 */
export async function registered(page: Page, number: number): Promise<RegisteredPR> {
  const credential = await token()
  const base = 'http://127.0.0.1:5175'
  const response = await page.request.post(base + '/api/open', { headers: { Authorization: `Bearer ${credential}` }, data: { url: `https://github.com/ui/fixture/pull/${number}` } })
  const opened: { prID: string; route: string } = await response.json()
  const metadata = await page.request.get(base + '/api/reviews/' + opened.prID, { headers: { Authorization: `Bearer ${credential}` } })
  const pr: RegisteredPR = await metadata.json()
  await page.goto(`${opened.route}#token=${credential}`)
  await expect(page.getByRole('heading', { name: `Pull request #${number}`, exact: true })).toBeVisible()
  return pr
}

/**
 * Builds explicit renderer data, not a native Git result.
 * @param pr Registered browser fixture PR.
 * @param count Number of fixture changed files.
 * @param baseSHA Fixture base tip.
 * @param headSHA Fixture head commit.
 * @returns Complete pinned-looking renderer fixture.
 */
export function sourceFixture(pr: RegisteredPR, count = 40, baseSHA = 'a'.repeat(40), headSHA = 'c'.repeat(40)): SourceResponse {
  const revision = { prID: pr.id, baseBranch: 'release/target', baseSource: 'github' as const, baseSHA, headSHA, mergeBaseSHA: 'b'.repeat(40) }
  const revisionID = digest([pr.id, revision.baseSHA, revision.headSHA, revision.mergeBaseSHA].join('\n'))
  const files: SourceFile[] = []
  for (let index = 0; index < count; index++) {
    const id = digest(`${revisionID}/file/${index}`)
    const lines: SourceLine[] = []
    for (let n = 1; n <= 120; n++) lines.push({ id: digest(`${id}/line/${n}`), sign: n === 1 ? '+' : ' ', text: n === 1 ? '<script>literal source, never HTML</script> ' + 'long source '.repeat(40) : `const fixtureLine${n} = ${n}`, oldLine: n === 1 ? undefined : n - 1, newLine: n, noNewline: n === 120 })
    files.push({ id, status: 'modified', oldPath: `src/file${String(index).padStart(4, '0')}.ts`, newPath: `src/file${String(index).padStart(4, '0')}.ts`, oldMode: '100644', newMode: '100644', oldObject: 'd'.repeat(40), newObject: 'e'.repeat(40), kind: 'text', hunks: [{ id: digest(id + '/hunk'), old: { id: digest(id + '/old'), start: 1, count: 119 }, new: { id: digest(id + '/new'), start: 1, count: 120 }, section: '', lines }] })
  }
  return { state: 'ready', pr: { ...pr, title: `Offline source fixture #${pr.number}`, author: 'fixture-author' }, diff: { revision, revisionID, complete: true, files } }
}
