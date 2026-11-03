# Sheldon

**Don't just merge it. Understand it.**

A local pull-request explainer. **Real pinned Files changed + separate Slate UI previews**—agent integration, questions and durable review saving remain unimplemented.

## Toolchains

Go **1.26.5** · Node **26.5.0** · npm **11.17.0**

Browser checks also need Playwright Chromium and its system libraries. Tests use offline fixtures, not provider logins or paid requests. Real source loading needs native Git and existing repository access; optional authenticated `gh` resolves the actual PR target. No harness is needed for source browsing.

## Develop

```sh
npm ci --ignore-scripts
npm run dev # terminal 1: Vite
```

```sh
SHELDON_DEV_ORIGIN=http://127.0.0.1:5173 go run ./cmd/sheldon # terminal 2: Go
```

UI: <http://127.0.0.1:5173> · backend: `127.0.0.1:7331`. Stop any existing standalone instance first. The CLI opens the authenticated UI; after reloading, rerun the CLI in another terminal to reconnect.

Connect the runtime, choose **Open a PR**, then **Register and open PR**. Opening from an existing PR uses a separate browser tab; allow that tab to preserve the current pinned view. On the registered page choose **Load pinned source**. If GitHub metadata is unavailable, explicitly choose **Use Git-only fallback**, select a fetched upstream branch, then **Load selected base**. No default target is assumed. Registration and source errors are separate; retry is explicit. Real PR pages offer file filtering and a pinned unified diff, not Ask, Split, Questions, Quiz or Semantic review.

**Sample UI previews** opens the separate `/?preview=library` entry. Its Files changed, Questions, Quiz, Semantic review, All PRs and Configuration use fictional data and page-memory simulations, never real PR source or inference.

## Release

Build once, then run one binary (no Node needed at runtime; Ctrl+C stops it):

```sh
npm run build
./bin/sheldon
./bin/sheldon open https://github.com/owner/repo/pull/1
./bin/sheldon --help
```

Windows output: `bin/sheldon.exe`. Plain `go run`/`go build` omit the embedded UI; use Vite for development.

`sheldon open <url>` registers/reuses the PR in private `prs.json` under the OS data directory and opens `/reviews/<id>` on the existing backend. Source loads only after **Load pinned source**; registration/reconnection alone never fetches it. Each browser tab owns its routed PR and displayed revision. Plain `sheldon` opens the real PR entry page. Reloading requires reconnecting; rerun the command or enter the token from private `instance.lock`.

Choose **Refresh source** to fetch current tips for this tab only. The displayed diff stays readable while loading and on failure; file navigation never refreshes it. Git-only refresh keeps the explicitly selected branch. Commit pinning remains internal—there is no snapshot-history UI or historical-read API. Reload/disconnect clears page-memory source.

Source uses OS cache storage (`~/.cache/sheldon` on Linux, `%LOCALAPPDATA%\sheldon\cache` on Windows), not the project directory. Worktrees and diffs are bounded; unsafe cache state, unavailable history or oversized comparisons produce errors rather than partial source. Existing credential helpers/SSH routes remain trusted. A worktree is not an agent sandbox.

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
internal/httpapi/    Protected loopback registration, lookup and source API
internal/app/        Writer lifecycle, PR index and source orchestration
internal/githubpr/   Canonical PR/revision contracts (no fetching)
internal/github/     Optional gh target-branch resolution
internal/git/        Managed fetch, pinned worktrees and unified diffs
internal/ui/         Embedded release shell / asset-free development fallback
scripts/build.js     Frontend → embed copy → Go build
web/                 Vue + Slate shell/fixtures + browser tests
```

Further launches reuse the running instance. A private `instance.lock` lives in the OS data directory; do not delete it while Sheldon is running.

Validated on Linux/amd64 only.
