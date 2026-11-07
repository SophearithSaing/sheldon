# Semantic review contract

Semantic review is a revision-pinned reading aid, not a safety assessment. This
package owns source/result validation and separate generation; the app owns
private storage and lifetime, HTTP owns protected operations, and Vue owns reading
state. No question/quiz records or native sessions are borrowed.

## Source and grouping

Verify the original existing worktree and complete merge-base → head diff through
`git.Cache.QuestionSource`; never fetch/repair source while generating. Supply the
complete manifest, including unsupported files, within a 512 KiB prompt. Refuse
text-empty/oversized comparisons. Native JSON is limited to 128 KiB, 32 groups,
128 references total, 200 lines per reference, 160-byte titles and 4096-byte
explanations. Retained text hunks must fit the existing saved-context verifier's
1000-line/64-KiB quote budget; larger comparisons fail explicitly. References identify an exact file/hunk, old/new side and inclusive
range. Each includes a changed line, stays within one hunk and resolves to literal
source. A file/hunk can contribute distinct ranges to multiple groups.

Coverage counts unique changed **line IDs**, not touched files/hunks or surrounding
context. Reject overlapping changed-line assignments rather than implying shared
coverage. Derive ungrouped lines from the full diff; non-text changes remain
explicit in the ordinary file view. Keep the bounded complete diff once in the
result, with groups referencing it, rather than duplicating hunks per excerpt.
Saved graphs validate the inventory, references and remainder without Git.
Structural validity does not establish factual correctness.

## Lifetime and storage

One replacement slot per PR, at most 32 PR slots per writer; competing revisions
reject busy rather than queue. Each explicit generation uses a fresh restricted
session in a separately owned lazy OpenCode 2.1.4 service. Preparation/execution
has 4m50s plus the adapter's bounded 10s reconciliation. Unknown creation/delivery
blocks the slot for that writer lifetime; no replay or replacement-session retry.
Cancellation before commit entry prevents replacement once idle is proven;
after commit entry it cannot promise rollback. Disconnect/view departure ends
observation, not accepted work. Shutdown drains without new saves/retries.

`<data>/reviews/<prID>/semantic-review.json` holds the current complete result:
schema version, PR, monotonic uint64 file version, random review ID, complete
verified diff and validated groups. Limit private file/public read to 4 MiB.
Use checked private paths and synced temporary replacement, predecessor-version
checks and explicit checked reads after uncertain replacement. No journal,
durable generation status, reading position, drafts or power-loss certification.
Saved reads need no source/native service. Question/quiz storage stays independent.

## HTTP and UI

All operations retain registered PR scope, bearer/Host/Origin/no-store guards,
canonical IDs, query rejection and strict bounded JSON. Public versions/sequences
are decimal strings; private native details never appear in DTOs.

- `GET /api/reviews/<id>/semantic-review`: saved result and current status;
  no body/query; HEAD rejected before provider work.
- `POST /api/reviews/<id>/semantic-review/generations`: `{revision, expectedVersion}`,
  4 KiB, 202 pending runtime admission, not saved completion.
- `GET /api/reviews/<id>/semantic-review/generations/<generation>`: exact status.
- `POST .../generations/<generation>/cancel`: `{revisionID}`, 1 KiB, 202 feedback.

Finite status is capped at 4 KiB. Poll only an admitted active generation, at most
once per second and one request in flight. Saved/status snapshots are independent,
not an atomic combined observation. No admission retry/idempotency claim.

Saved groups open without inference; explicit regeneration confirms replacement.
Original-diff links and questions require matching currently verified source.
Selected ranges use the existing single-anchor question flow and shared revision
session. No whole-group multi-range question schema or historical-source API.
Preserve drafts, live answers, quiz attempts and focus; old results stay labeled
old after refresh. Reject obsolete replies by scope, request and version identity.

## Verification

Offline tests must cover cross-file/separate same-hunk ranges, overlap/invalid
references, derived remainder, complete inventory limits, private storage failures,
non-dispatching reads, generation/cancellation/commit races, original question
anchors, stale browser callbacks and PR/revision isolation. Final development and
clean-release flows use actual Git/application HTTP/storage with synthetic native
inference, not browser-mocked success. Native Linux/Chromium evidence does not
certify live-provider quality, non-Linux behavior, sandboxing or exactly-once work.
