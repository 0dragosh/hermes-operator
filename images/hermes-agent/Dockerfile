# images/hermes-agent/Dockerfile
#
# Two-stage multi-arch build for ghcr.io/paperclipinc/hermes-agent.
# Stage 1 (`builder`) uses the official uv image to resolve and install the pinned
# Python environment into a self-contained venv at /opt/venv. Stage 2 (`runtime`)
# copies only the venv + minimal apt deps and runs as the non-root `hermes` user.
#
# Build args:
#   HERMES_VERSION  : the exact hermes-agent git tag to label. Defaults to the
#                     repository-pinned tag; CI overrides it from the matrix.
#   PYTHON_IMAGE    : digest-pinned Python runtime image.
#   UV_IMAGE        : digest-pinned uv image copied into the builder.
#   TINI_VERSION    : tini release tag (lesson openclaw #471: tini as PID 1).
#
# Output: a runtime image that, with no further configuration, runs
#         `hermes-agent --config ~/.hermes/config.yaml` as UID 1000.

ARG PYTHON_IMAGE=docker.io/library/python:3.11-slim-bookworm@sha256:8dca233de9f3d9bb410665f00a4da6dd06f331083137e0e98ccf227236fcc438
ARG UV_IMAGE=ghcr.io/astral-sh/uv:0.11.16@sha256:440fd6477af86a2f1b38080c539f1672cd22acb1b1a47e321dba5158ab08864d
ARG TINI_VERSION=v0.19.0
ARG HERMES_VERSION=v2026.5.29.2

# ---------- Stage 1: builder ----------
FROM ${UV_IMAGE} AS uv

FROM ${PYTHON_IMAGE} AS builder
ARG HERMES_VERSION

# uv binary from the dedicated stage above.
COPY --from=uv /uv /uvx /usr/local/bin/

ENV UV_LINK_MODE=copy \
    UV_COMPILE_BYTECODE=1 \
    UV_PYTHON_DOWNLOADS=never \
    UV_PROJECT_ENVIRONMENT=/opt/venv \
    VIRTUAL_ENV=/opt/venv

WORKDIR /build

# git + ca-certificates are needed at lock-resolve time because the lockfile
# pins hermes-agent by git ref (upstream is not on PyPI).
RUN apt-get -o APT::Sandbox::User=root update \
    && apt-get -o APT::Sandbox::User=root install -y --no-install-recommends git ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# Copy lockfile + project metadata first for layer caching.
COPY pyproject.toml uv.lock ./

# Sync the frozen lockfile. `make agent-image-relock HERMES_VERSION=vX.Y.Z`
# rewrites the @ ref in pyproject.toml and regenerates uv.lock; at build time
# we simply trust the lock. HERMES_VERSION is informational for the labels stage.
RUN --mount=type=cache,target=/root/.cache/uv \
    set -eux; \
    if [ -z "${HERMES_VERSION}" ]; then \
        echo "ERROR: HERMES_VERSION build-arg is required" >&2; exit 1; \
    fi; \
    uv sync --frozen --no-dev

# ---------- Stage 2: runtime ----------
FROM ${PYTHON_IMAGE} AS runtime
ARG TINI_VERSION
ARG HERMES_VERSION
ARG TARGETARCH

# Runtime apt packages:
#   - ffmpeg, ripgrep: hard dependencies of hermes-agent (audio + search)
#   - git, openssh-client: hermes-agent skill installs are git-based
#   - awscli, zstd, tar, coreutils: backup/restore object transfer tools
#   - ca-certificates: TLS to platform gateways
#   - tini: PID 1 reaper (lesson openclaw-operator #471)
RUN apt-get -o APT::Sandbox::User=root update \
    && apt-get -o APT::Sandbox::User=root install -y --no-install-recommends \
        awscli \
        ffmpeg \
        ripgrep \
        git \
        openssh-client \
        zstd \
        tar \
        coreutils \
        ca-certificates \
        tini \
    && rm -rf /var/lib/apt/lists/*

# Non-root user matching the StatefulSet's runAsUser=1000 (Plan 1).
RUN groupadd --system --gid 1000 hermes \
    && useradd --system --uid 1000 --gid 1000 --create-home --shell /usr/sbin/nologin hermes \
    && mkdir -p /home/hermes/.hermes \
    && chown -R hermes:hermes /home/hermes

# Copy the resolved venv from the builder stage.
COPY --from=uv /uv /uvx /usr/local/bin/
COPY --from=builder --chown=hermes:hermes /opt/venv /opt/venv
COPY --from=builder --chown=hermes:hermes /build/pyproject.toml /build/uv.lock /opt/venv-template/
ENV PATH="/opt/venv/bin:${PATH}" \
    PYTHONUNBUFFERED=1 \
    PYTHONDONTWRITEBYTECODE=1 \
    HOME=/home/hermes

# Image metadata. The HERMES_VERSION label is the one the operator's autoupdate
# controller (Plan 5) compares against the registry tag.
LABEL org.opencontainers.image.title="hermes-agent" \
      org.opencontainers.image.source="https://github.com/paperclipinc/hermes-operator" \
      org.opencontainers.image.documentation="https://github.com/paperclipinc/hermes-operator/blob/main/images/hermes-agent/README.md" \
      org.opencontainers.image.licenses="MIT" \
      org.opencontainers.image.vendor="paperclipinc" \
      hermes.agent/version="${HERMES_VERSION}"

USER hermes
WORKDIR /home/hermes

COPY --chown=hermes:hermes entrypoint.sh /usr/local/bin/hermes-entrypoint
RUN chmod +x /usr/local/bin/hermes-entrypoint

# tini as PID 1 to reap any subprocess hermes-agent forks (audio transcoders,
# git operations, skill executors). `--` separates tini's flags from the wrapped
# entrypoint script.
ENTRYPOINT ["/usr/bin/tini", "--", "/usr/local/bin/hermes-entrypoint"]
CMD ["serve"]
