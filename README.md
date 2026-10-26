# Sheldon

**Don't just merge it. Understand it.**

A local pull-request explainer. **Scaffold only**—server, PR loading and agent integration are not implemented yet.

## Toolchains

Go **1.26.5** · Node **26.5.0** · npm **11.17.0**

## Develop

```sh
npm ci --ignore-scripts
npm run dev
```

Frontend: <http://127.0.0.1:5173>

CLI (help/version only):

```sh
go run ./cmd/sheldon --help
go run ./cmd/sheldon --version
```

## Check & build

```sh
go test -race ./...
go vet ./...
go build ./...
npm run typecheck
npm run build:web
```

Frontend output: `web/dist/` (not yet embedded in Go).

## Layout

```text
cmd/sheldon/    Go CLI + tests
web/           Vue 3 + TypeScript + Vite
```

Validated on Linux/amd64 only.
