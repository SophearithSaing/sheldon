# Sheldon

**Don't just merge it. Understand it.**

A local pull-request explainer. **Runtime + frontend fixtures only**—PR loading and agent integration are not implemented yet.

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

## Release

Build once, then run one binary (no Node needed at runtime; Ctrl+C stops it):

```sh
npm run build
./bin/sheldon
./bin/sheldon open https://github.com/owner/repo/pull/1
./bin/sheldon --help
```

Windows output: `bin/sheldon.exe`. Plain `go run`/`go build` omit the embedded UI; use Vite for development.

After connecting, open **Environment & diagnostics** to check Git, optional `gh`, OpenCode, Codex and Claude Code. Only bounded version/help commands run—no inference, login, installation or credential inspection. Advertised features do not prove compatibility, service health, login or quota; unverified checks stay **unknown**. Use trusted executables on PATH; these checks are not a sandbox.

Existing harness installations may have their own runtime prerequisites.

**Settings & Environment** opens the setup scaffold after connecting. Saving, native model discovery and real Test answers remain unavailable. Preferences are intended for **new conversations only**; existing native session/model bindings must not change.

Settings UI fixtures (no backend, login or inference):

```sh
npm run dev --workspace=@sheldon/web -- --mode fixture
# http://127.0.0.1:5173/?fixture=settings
```

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
npm run test:release # isolated binary + browser lifecycle checks (Linux/macOS)
```

[Foundation CI](.github/workflows/ci.yml) runs these checks on Ubuntu 24.04, starting without generated assets. It also compiles Windows/amd64 and macOS/arm64 tests; that is **not native platform validation**. CI never runs live agent experiments and does not upload private test artifacts.

## Layout

```text
cmd/sheldon/          Go CLI + tests
internal/platform/   OS storage paths
internal/process/    Owned command execution + cancellation
internal/httpapi/    Protected loopback REST/SSE foundation
internal/app/        Launch, browser handoff + single-writer lifecycle
internal/environment/ Safe local CLI metadata + setup diagnostics
internal/settings/   Preference/model/session-binding contracts (no persistence yet)
internal/githubpr/   GitHub PR URL validation (no fetching)
internal/ui/         Embedded release shell / asset-free development fallback
scripts/build.js     Frontend → embed copy → Go build
web/                 Vue + Graphite fixtures + browser tests
```

Further launches reuse the running instance. A private `instance.lock` lives in the OS data directory; do not delete it while Sheldon is running.

Validated on Linux/amd64 only.
