# Quiz generation

Explicit generation verifies the existing registered PR, pinned merge-base/head
comparison and original worktree without fetching or repair. The complete diff
stays backend-side for evidence validation; the prompt contains only instructions,
canonical PR URL and exact revision, bounded at 16 KiB. Diff size no longer counts
against an upfront 512-KiB prompt budget.

The separately owned lazy quiz service uses the same repository-investigation
permission profile as semantic generation (`opencode.StartInvestigation`), not
the shared conversation service. Read/glob/grep and narrowly allowed read-only
Git/GitHub command forms are available. Local pinned Git is authoritative;
optional `gh pr view/diff` is context only and never substitutes a newer revision.
Question conversations keep their original read/search-only permissions. Startup
hard policies, effective-session checks and child-only Git controls remain in
place. This is best-effort restriction, not a sandbox; no global configuration,
provider setup, login or harness installation is changed.

Native output contains exactly five questions, four choices each, one private
answer key, concise explanation and one or two evidence ranges. References use
exact old/new repository paths and inclusive coordinates. Sheldon resolves them
to its own revision-scoped file/hunk IDs and derives literal evidence; generated
IDs/code, foreign paths, wrong rename sides, ambiguous ranges, context-only and
cross-hunk selections fail validation. Native output is capped at 128 KiB.

Answer-key withholding, checking without inference, predecessor-version guards,
cancellation, uncertainty, restart and private `reviews/<id>/quiz.json` storage
are unchanged. Newly generated evidence stores only the selected supporting
lines, exact original anchor and path labels; it omits `surrounding`. Schema-1
quizzes with legacy surrounding hunks still read, validate and grade without
migration or automatic rewriting. Checked responses accept either evidence shape;
the UI renders the selected lines and keeps original-diff navigation. Saved content
is bounded at 2 MiB, without retaining the entire PR diff or unselected context.

Before saving, references are resolved against the complete original diff, not a
cropped reconstruction. Saved compact evidence checks contiguous side-specific
coordinates, changed-line eligibility, unique line IDs and literal text. Repeated
or overlapping retained lines must agree across questions, including mixed legacy
and compact evidence. Saved quotes alone do not grant access to newer/missing source
or certify factual correctness. Reading/checking needs no Git or inference.

## Remaining bounds

- Source loader: 4-MiB native Git command output, 1000 changed files and 100,000
  patch lines per file, inherited from the existing Git layer.
- Evidence: 200 selected lines, 8 KiB per excerpt counting only selected source
  and path labels; 64 KiB total, counting repeated selections. A huge surrounding
  hunk no longer blocks a small supporting selection. The requested selection is
  never clipped or expanded with arbitrary nearby lines. Legacy surrounding-hunk
  records retain their original validation and byte accounting.
- Generation: 4m50s plus bounded native reconciliation; one replacement per PR,
  with no automatic prompt replay, batching or exhaustive-investigation guarantee.

Offline tests use actual pinned Git plus synthetic native inference. They verify
compact prompts, small evidence inside a large native hunk, validated path-to-anchor
mapping, independent conversation permissions and compatible save/check/restart
behavior. They do not certify factual question quality, live-provider compatibility,
native non-Linux platforms or complete native command confinement.
