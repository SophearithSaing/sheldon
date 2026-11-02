# Sheldon

**Don't just merge it. Understand it.**

A local pull-request explainer. **Local PR registration + Slate UI previews**—PR pages do not load source or diffs yet; agent integration and durable review saving remain unimplemented.

## Toolchains

Go **1.26.5** · Node **26.5.0** · npm **11.17.0**

Browser checks also need Playwright Chromium and its system libraries. Agent/GitHub checks use offline fixtures, not provider logins or paid requests. The planned review flow needs Git and a compatible externally installed/authenticated harness; `gh` is optional.

## Develop

```sh
npm ci --ignore-scripts
npm run dev # terminal 1: Vite
```

```sh
SHELDON_DEV_ORIGIN=http://127.0.0.1:5173 go run ./cmd/sheldon # terminal 2: Go
```

UI: <http://127.0.0.1:5173> · backend: `127.0.0.1:7331`. Stop any existing standalone instance first. The CLI opens the authenticated UI; after reloading, rerun the CLI in another terminal to reconnect.

The root UI uses fictional sample data. Files changed, Questions, Quiz, Semantic review, All PRs and Configuration keep state only in page memory. Generation/creation controls are explicitly labeled local simulations; real Send, Save, discovery and UI PR import remain unavailable. Semantic review never starts work on entry.

## Release

Build once, then run one binary (no Node needed at runtime; Ctrl+C stops it):

```sh
npm run build
./bin/sheldon
./bin/sheldon open https://github.com/owner/repo/pull/1
./bin/sheldon --help
```

Windows output: `bin/sheldon.exe`. Plain `go run`/`go build` omit the embedded UI; use Vite for development.

`sheldon open <url>` registers/reuses the PR in private `prs.json` under the OS data directory and opens `/reviews/<id>` on the existing backend. Each browser tab reads only its routed PR; opening another PR does not replace it. These pages show registration only, not a fetched diff. Plain `sheldon` opens the sample-UI root. Reloading either page requires reconnecting; rerun the original command or enter the token from private `instance.lock`.

## Check & build

```sh
go test -race ./...
go vet ./...
go build ./...
npm run typecheck
npm run build
go test -race -tags=release ./...
go vet -tags=release ./...
```

`npm run build` builds `web/dist/`, copies it to `internal/ui/dist/`, then compiles with `-tags=release`. Generated assets and binaries stay uncommitted.

Browser tests:

```sh
npm exec --workspace=@sheldon/web -- playwright install chromium # first run
npm run test:web
npm run test:release # clean source build + isolated binary/browser checks (Linux/macOS)
```

[Foundation CI](.github/workflows/ci.yml) runs these checks on Ubuntu 24.04, starting without generated assets. It also compiles Windows/amd64 and macOS/arm64 tests; that is **not native platform validation**. CI never runs live agent experiments and does not upload private test artifacts.

Release browser checks build a disposable application-source copy without `refs/` or existing bundles, using installed dependencies. They remove the copy before running only the binary with private temporary storage and no Node on its PATH. This does not regenerate the repository's embedded assets; run `npm run build` before release-tag Go checks.

## Layout

```text
cmd/sheldon/          Go CLI + tests
internal/platform/   OS storage paths
internal/process/    Owned command execution + cancellation
internal/httpapi/    Protected loopback API + registered PR lookup
internal/app/        Launch, browser handoff, writer lifecycle + durable PR index
internal/githubpr/   Canonical PR/revision contracts (no fetching)
internal/github/     Optional gh base-resolution backend (not yet connected)
internal/git/        Managed fetch, merge-base and pinned-worktree backend (UI pending)
internal/ui/         Embedded release shell / asset-free development fallback
scripts/build.js     Frontend → embed copy → Go build
web/                 Vue + Slate shell/fixtures + browser tests
```

Further launches reuse the running instance. A private `instance.lock` lives in the OS data directory; do not delete it while Sheldon is running.

Validated on Linux/amd64 only.
