# hermes-agent image build context

The operator owns `ghcr.io/paperclipinc/hermes-agent`. Upstream
(`nousresearch/hermes-agent`) ships only a Python package, so this directory
packages it into a multi-arch container that the operator can pull by default.

## Layout

| File | Purpose |
|---|---|
| `Dockerfile` | Multi-stage build (uv builder + slim runtime with ffmpeg, ripgrep, git, ssh, awscli, zstd, tar, coreutils, certs, and tini). |
| `pyproject.toml` | uv project pinning `hermes-agent`. |
| `uv.lock` | Committed lockfile: reproducible builds. |
| `entrypoint.sh` | tini-wrapped startup; sources `~/.hermes/config.yaml`. |

## Common workflows

```bash
# Bump the pinned upstream version and refresh the lockfile.
make agent-image-relock HERMES_VERSION=v2026.5.29.2

# Build locally for the current platform.
make agent-image-build HERMES_VERSION=v2026.5.29.2

# Smoke-test the local build.
make agent-image-smoke HERMES_VERSION=v2026.5.29.2
```

`images/hermes-agent/uv.lock` is required. CI treats the committed lockfile as
the source of truth and fails if it is missing or does not match the requested
`HERMES_VERSION`. The requested `HERMES_VERSION` must exist as an upstream git
tag. If Docker, Podman, or network access is unavailable locally, run the relock
command in a CI-capable environment and commit the generated lockfile. Do not
hand-write a replacement lockfile.

CI builds the matrix in `.github/workflows/agent-image.yaml`, signs each image
with Cosign (keyless OIDC), and attaches an SBOM via Syft.
