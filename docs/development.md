# Local Development

## Toolchain

Use Go 1.26.x. The module declares `go 1.26.0`.

A Nix shell with the common local tools:

```bash
nix shell nixpkgs#go_1_26 nixpkgs#kubectl nixpkgs#kubernetes-helm nixpkgs#kind nixpkgs#jq nixpkgs#yq-go
```

If your base environment does not already provide `make`, `bash`, or a C
compiler for `go install`, include those explicitly:

```bash
nix shell nixpkgs#go_1_26 nixpkgs#gnumake nixpkgs#bash nixpkgs#gcc nixpkgs#kubectl nixpkgs#kubernetes-helm nixpkgs#kind nixpkgs#jq nixpkgs#yq-go
```

If your base environment does not already provide `make` and `bash`, add `nixpkgs#gnumake nixpkgs#bash` to the shell command.

The Makefile pins repository-local tools under `./bin`:

| Tool | Makefile variable | Version |
| --- | --- | --- |
| kustomize | `KUSTOMIZE_VERSION` | `v5.4.2` |
| controller-gen | `CONTROLLER_TOOLS_VERSION` | `v0.21.0` |
| setup-envtest | `ENVTEST_VERSION` | `release-0.24` |
| golangci-lint | `GOLANGCI_LINT_VERSION` | `v2.12.2` |
| yq | `YQ_VERSION` | `v4.45.1` |
| operator-sdk | `OPERATOR_SDK_VERSION` | `v1.38.0` |
| opm | `OPM_VERSION` | `v1.47.0` |

Install the pinned `yq` used by RBAC verification:

```bash
make yq
```

## Supply Chain Pins

Container base images are pinned by digest in `Dockerfile` and
`images/hermes-agent/Dockerfile`. The agent relock helper also pins its uv
container in `AGENT_UV_IMAGE`.

When updating those images:

```bash
skopeo inspect --format '{{.Digest}}' docker://docker.io/library/golang:1.26
skopeo inspect --format '{{.Digest}}' docker://gcr.io/distroless/static:nonroot
skopeo inspect --format '{{.Digest}}' docker://docker.io/library/python:3.11-slim-bookworm
skopeo inspect --format '{{.Digest}}' docker://ghcr.io/astral-sh/uv:0.11.16
skopeo inspect --format '{{.Digest}}' docker://ghcr.io/astral-sh/uv:0.11.16-python3.11-trixie
```

Update the tag and digest together, then run the Dockerfile and image smoke
checks for the changed image.

## Test Targets

`make test-readonly` runs fast unit tests that do not generate or rewrite files:

```bash
CGO_ENABLED=0 go test ./api/... ./internal/resources/... ./internal/oci/... ./internal/webhook/... -count=1
```

`make test-controller` runs controller envtest specs with Kubernetes API server assets downloaded by `setup-envtest`:

```bash
make test-controller
```

`make test` is the full developer target. It runs `make manifests`, `make generate`, `make fmt`, `make vet`, downloads envtest assets, and then runs the non-e2e Go test suite with coverage output. Because it regenerates files, use it when you want the full local validation pass.

## Envtest Assets

The default envtest Kubernetes version is controlled by `ENVTEST_K8S_VERSION` in the Makefile. The current default is `1.33.0`.

For controller tests, the Makefile resolves assets automatically:

```bash
KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.33.0 --bin-dir ./bin -p path)" CGO_ENABLED=0 go test ./internal/controller/... -count=1 -ginkgo.v
```

To prepare assets for IDE-driven test runs:

```bash
make envtest
./bin/setup-envtest use 1.33.0 --bin-dir ./bin
export KUBEBUILDER_ASSETS="$(./bin/setup-envtest use 1.33.0 --bin-dir ./bin -p path)"
```

The controller test suite also searches `./bin/k8s` for downloaded envtest assets when `KUBEBUILDER_ASSETS` is not set.
