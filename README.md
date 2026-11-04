# Sheldon

**Don't just merge it. Understand it.**

A local pull-request explainer. **Real pinned Files changed + saved questions/follow-ups + separate Slate UI previews**—submitted questions and finished answers are saved locally with explicit save-failure feedback and PR-local reopening.

## Toolchains

Go **1.26.5** · Node **26.5.0** · npm **11.17.0**

Browser checks also need Playwright Chromium and its system libraries. Tests use offline fixtures, not provider logins or paid requests. Real source loading needs native Git and existing repository access; optional authenticated `gh` resolves the actual PR target. No harness is needed for source browsing. Real questions require an already-installed **OpenCode 2.1.4** with existing provider/default-model configuration; Sheldon does not install or configure it. Current question integration evidence is offline, not a live-provider compatibility claim.

## Develop

```sh
npm ci --ignore-scripts
npm run dev # terminal 1: Vite
```

```sh
SHELDON_DEV_ORIGIN=http://127.0.0.1:5173 go run ./cmd/sheldon # terminal 2: Go
```

UI: <http://127.0.0.1:5173> · backend: `127.0.0.1:7331`. Stop any existing standalone instance first. The CLI opens the authenticated UI; after reloading, rerun the CLI in another terminal to reconnect.

Connect the runtime, choose **Open a PR**, then **Register and open PR**. Opening from an existing PR uses a separate browser tab; allow that tab to preserve the current pinned view. On the registered page choose **Load pinned source**. If GitHub metadata is unavailable, explicitly choose **Use Git-only fallback**, select a fetched upstream branch, then **Load selected base**. No default target is assumed. Registration and source errors are separate; retry is explicit. Real PR pages offer file filtering, a pinned unified diff and saved Questions. Split, Quiz and Semantic review remain unavailable.

For a text diff, choose gutter line numbers (Shift extends on one side within one hunk) or **Select hunk**, then **Ask about selection**. The inline composer is pinned to that file/side/range/revision. **Send question** or Ctrl/Cmd+Enter explicitly starts backend work; Enter inserts a newline. Cancel retains the draft; resume or explicitly discard it before changing files/selection or refreshing. Native text selection/copy never submits. Non-text changes have no line questions.

Successful admission adds one marker and opens its conversation with streamed, escaped answer text, pinned context and **Stop this answer**. Admission and Stop acknowledgements are not completion proof. Follow-ups use **Enter to send / Shift+Enter for a newline**, respect IME composition, and retain each thread's own transcript and draft when switching threads/files. Model-written citations remain plain text, not trusted navigation links.

The shared revision allows one active question; drafts stay editable while sending is unavailable. Backend busy rejection also covers other tabs; retry is explicit, not synchronized thread-library state or a cross-tab exactly-once guarantee. Rejected requests retain drafts without phantom messages. Uncertain admission disables resending. **Reconnect answer observation** only checks the original answer after stream loss—it never resubmits a question. Closing the panel does not stop execution. Disconnecting a question workspace asks whether to keep connected or explicitly discard page state; it never implies native cancellation. Reload/disconnect clears page-memory drafts, transcripts and markers, not accepted backend execution or saving. Execution and saving have separate statuses: Completed does not mean Saved.

Before sending a native prompt, Sheldon records its submitted question, pinned context and exact shared binding in private `reviews/<id>/threads.json` under the OS data directory. Answers save when execution ends (completed, failed or interrupted), before another question can use that revision. There are no periodic progress writes or shutdown saves/retries: in-progress text stays in memory and is lost on exit, while its submitted question remains saved. Save failure retains runtime text and blocks further questions on that revision. **Retry saving** writes only retained text/status to disk, never resends a question. Drafts remain unsaved. Open **Questions**, then a saved row to reopen history after reload/reconnect/restart, even before loading source. Failed or uncertain native initialization stays unavailable rather than creating another session. No prompt is replayed to recover missing state.

After restart, the first backend history read for a PR (including admission's storage check) classifies its saved Pending/Streaming answers as **Interrupted / execution unknown**, retaining any saved text and original context. This classification is persisted once, without Git/source loading, agent startup or prompt replay; it does not prove native cancellation or idle. Corrupt history or a failed classification save returns an error, never empty healthy history. Current-run answers and existing terminal answers are unchanged.

After an abrupt exit, reopen **Questions** to inspect what was actually saved. Previously saved failed/interrupted partial answers remain readable; an unfinished answer may have no saved output because streaming progress is never written periodically. Missing original source/native state or unresolved prior execution prevents continuation, not reading. Do not resend an old question to recover it.

Use **Refresh saved questions** or **Refresh saved thread** to read other tabs' submissions explicitly. Reads use authenticated, no-store `GET /api/reviews/<pr-id>/threads` and `GET /api/reviews/<pr-id>/threads/<thread-id>`, returning bounded saved data with decimal-string versions and separate current saving/session feedback—never native bindings, host paths or credentials. They start no source or native work. Opening an active/unsaved answer observes only its exact existing answer; it never resends input. Refresh retains live text, drafts and selection. Switching between Questions and Files changed retains the inline draft on its original selection.

Type a **Follow up in this thread** to continue explicitly after restart. Sending verifies the original existing source, native session identity/location/model/restrictions, exact saved input history, terminal idle boundaries and an empty inbox before saving/sending the **new** question. Current model defaults are not consulted. Missing/incompatible source or native state, truncated history and uncertain execution block continuation without replacement, prompt replay or source repair. Saved transcripts and quotes remain readable. Historical Interrupted/unknown answers are not rewritten as completed merely because the session can now continue.

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

Choose **Refresh source** to fetch current tips for this tab only. The displayed diff stays readable while loading and on failure; file navigation never refreshes it. Git-only refresh keeps the explicitly selected branch. Existing threads and follow-up drafts retain their original revision/selection; the panel labels older context. **Open conversation** reopens retained threads even after their markers no longer belong to the displayed diff. Inline drafts must be finished or discarded before refresh. Commit pinning remains internal—there is no snapshot-history UI or historical-source API. Reload/disconnect clears page-memory source and drafts, not saved history or accepted execution.

Source uses OS cache storage (`~/.cache/sheldon` on Linux, `%LOCALAPPDATA%\sheldon\cache` on Windows), not the project directory. Worktrees and diffs are bounded; unsafe cache state, unavailable history or oversized comparisons produce errors rather than partial source. Existing credential helpers/SSH routes remain trusted. A worktree is not an agent sandbox.

Question backend operations are explicit and authenticated; browsing never starts inference. Execution uses the pinned OpenCode 2.1.4 adapter and existing provider setup, without installing or configuring either. Browser disconnect stops observation, not execution. A worker has a 4m50s deadline plus up to 10s of reconciliation; Stop targets its active answer/session and is not completion proof. Native stream loss permits bounded history checks, never prompt replay. An unknown current outcome blocks that shared revision session; shutdown cancels owned activity. Durable admission alone proves neither native completion nor safe continuation; original-session verification remains mandatory.

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
npm run test --workspace=@sheldon/web -- --config playwright.release.config.ts release-source.spec.ts # native offline PR flow only
npm run test --workspace=@sheldon/web -- development-question.spec.ts # actual Go/Vite/Git + synthetic native answers
npm run test --workspace=@sheldon/web -- --config playwright.release.config.ts release-question.spec.ts # same flow through clean embedded release
```

Run the frontend and release browser suites separately; they share Playwright's output directory.

[Foundation CI](.github/workflows/ci.yml) runs these checks on Ubuntu 24.04, starting without generated assets. It also compiles Windows/amd64 and macOS/arm64 tests; that is **not native platform validation**. CI never runs live agent experiments and does not upload private test artifacts.

Release browser checks build a disposable application-source copy without `refs/` or existing bundles, using installed dependencies. They remove the copy before running only the binary with private temporary storage and no Node on its PATH. This does not regenerate the repository's embedded assets; run `npm run build` before release-tag Go checks.

The native-source release test needs Go and Git to build disposable tools and local PR objects. It exercises two PRs, explicit Git-only base selection, refresh/retry, restart/cache reuse and active-fetch shutdown through the standalone CLI. Test-only `gh`, Git transport and desktop-opener fixtures stay offline; source HTTP responses are not mocked. No GitHub login, provider setup or live requests are needed.

The question integration tests use actual browser actions, managed native Git source and production HTTP/SSE handlers, without browser HTTP mocks. A disposable Go executable simulates only the native OpenCode protocol/inference. Development and clean-source release flows cover shared-session threads/follow-ups, cross-tab observation/busy rejection, separate PRs, targeted Stop, failure/recovery, reload/reconnect and app/service restart. Saved rows reopen through the UI and explicitly continue the exact original session, without session replacement/default lookup/prompt replay. Abrupt-exit cases retain saved partial outcomes, classify unfinished turns honestly and fail closed with missing source/native state or unresolved execution. Release application processes have no Node on PATH. Real-provider restart compatibility remains unverified.

## Layout

```text
cmd/sheldon/          Go CLI + tests
internal/platform/   OS storage paths
internal/process/    Owned command execution + cancellation
internal/httpapi/    Protected loopback PR/source and question/SSE/Stop API
internal/app/        Writer lifecycle, PR index, private admissions and source orchestration
internal/githubpr/   Canonical PR/revision contracts (no fetching)
internal/github/     Optional gh target-branch resolution
internal/git/        Managed fetch, pinned worktrees and unified diffs
internal/question/   Durable admission contracts, pinned context and runtime answer execution
internal/opencode/   Restricted native service/session, execution and history adapter
internal/ui/         Embedded release shell / asset-free development fallback
scripts/build.js     Frontend → embed copy → Go build
web/                 Vue + Slate shell/fixtures + browser tests
```

Further launches reuse the running instance. A private `instance.lock` lives in the OS data directory; do not delete it while Sheldon is running.

Validated on Linux/amd64 only.
