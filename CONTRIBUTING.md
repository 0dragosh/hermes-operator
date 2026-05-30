# Contributing

## Development

- Go 1.26.x, kubebuilder v4, kind, helm, and the Makefile-pinned toolchain.
- See [`docs/development.md`](docs/development.md) for local setup, test targets, and envtest assets.
- `make test-readonly` runs non-mutating unit tests. `make test-controller` runs controller envtest. `make test` remains the full mutating developer target.
- `make lint` runs golangci-lint.
- `make sync-chart-crds` after `make manifests` (CI enforces this).
- Use conventional commits: `feat:` `fix:` `docs:` `ci:` `chore:` `refactor:` `test:`.
- Use git worktrees rather than switching branches in the main checkout (`git worktree add ../hermes-operator-<suffix> -b <branch> main`).

## Reconciliation rules

See [`docs/conventions.md`](docs/conventions.md). The `Reconcile Guard` CI job enforces a subset; you are responsible for the rest.
