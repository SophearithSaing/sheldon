# Agent guide

## Coding conventions

- Keep MVP code focused, idiomatic and consistent with nearby code. Avoid speculative abstractions, unused future types and production hooks added only for tests.
- Format Go with `gofmt`; document non-test functions/methods with a short and concise GoDoc. Use direct `os.Getenv` and `t.Setenv`, not injected environment getters.
- Use Vue/TypeScript and ES-module `.js` scripts. Keep dependencies/toolchains pinned and use the npm lockfile.
- Use OS storage directories, not project-local `.sheldon/`. Keep experiments in `refs/experiments/` and generated assets/binaries uncommitted.
- Keep README command-focused. Number dev logs by milestone, not checkpoint.

### TypeScript documentation

- Every function must have an explicit return type and a TSDoc header.
- Include `@param` for every parameter. Include `@returns` only when returning a value; omit it for `void` functions (never write `@returns Nothing.`).
- Body comments are forbidden except for complex algorithmic logic in long functions or non-obvious workarounds for third-party bugs.
- Keep TSDoc concise, following these examples:

```ts
/**
 * Finds an item by id.
 * @param itemId Item id to find.
 * @returns Matching item, or null when no item exists.
 */
private findItem(itemId: string): Item | null {
  return null;
}

/**
 * Clears the current state.
 */
public clearState(): void {
  this.state.set(null);
}
```

## Verification

- Use the pinned toolchains; install dependencies with `npm ci --ignore-scripts`.
- Go changes: run relevant tests, then `go test -race ./...` and `go vet ./...`.
- Frontend changes: run `npm run typecheck`, `npm run test:web` and inspect relevant screenshots/UI states.
- Release/integration changes: run `npm run test:release` (rebuilds the embedded binary). Run `npm run build` before `go test -race -tags=release ./...` and `go vet -tags=release ./...`.
- Workflow changes: use `actionlint` when available. Check whitespace with `git diff --check`.
- Report checks actually run and their limits; cross-compilation is not native validation. See README for full commands/prerequisites.
- Use offline fixtures by default. Live inference, provider login, harness installation/update or global harness/provider-config changes require explicit user approval. Never expose credentials or claim fixtures/unimplemented actions are real success.
