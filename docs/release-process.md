# Release Process

> The release pipeline is fully automated except for: (1) installing the
> release bot GitHub App once, (2) merging the release-please PR. Everything
> else fires on its own.

## One-time setup

See Plan 6, Task 1. Summary:

- Repository variable `RELEASE_BOT_APP_ID` contains the release bot App ID.
- Repository secret `RELEASE_BOT_PRIVATE_KEY` contains the release bot private
  key.
- The release bot App can write contents and pull requests in
  `paperclipinc/hermes-operator`.
- Workflow permissions: default-write enabled.
- Packages: write enabled.

Calendar a yearly reminder to rotate the App private key; the weekly
`verify-signing.yaml` workflow will not catch stale release bot credentials
because it only verifies already-published releases.

## Per-release flow

1. **Land conventional commits to `main`.** `feat:` and `fix:` show up in the
   changelog. `docs:`, `chore:`, `ci:`, `test:`, `build:`, `refactor:`, `perf:`
   are hidden but counted.
2. **release-please opens a release PR** named
   `chore(main): release vX.Y.Z`. The PR contains:
   - `CHANGELOG.md` update
   - `.release-please-manifest.json` bump
   - `charts/hermes-operator/Chart.yaml` version + appVersion bump
   - `charts/hermes-operator/values.yaml` image.tag bump
   - `bundle/manifests/hermes-operator.clusterserviceversion.yaml` version +
     metadata.name + containerImage bump
   The CSV fields must resolve to `metadata.name: hermes-operator.vX.Y.Z`,
   `metadata.annotations.containerImage:
   ghcr.io/paperclipinc/hermes-operator:vX.Y.Z`, and
   `spec.version: X.Y.Z`. Release tagging and OperatorHub submission refuse to
   continue if those fields are reduced to bare version strings.
3. **Review and merge the PR.** Squash-and-merge is fine; the commit subject
   must remain `chore(main): release vX.Y.Z` for the tag creator step to
   recognise it.
4. **release-please.yaml's "Create release tag" step** detects the merge
   commit, validates the CSV release fields above, and pushes `vX.Y.Z` via the
   release bot App token. The App-authored push fires
   downstream workflows.
5. **release.yaml** fires on the tag, runs the release gates, and only then
   publishes artifacts. Required gates are:
   - `make test-readonly`
   - `make manifests generate` followed by a dirty-tree check
   - `bash hack/check-helm-rbac.sh`
   - `make sync-bundle-rbac-check`
   - `bash hack/check-distribution-parity.sh` when that script exists
   - `make bundle-validate`
   - The reusable conformance workflow
6. **release.yaml** publishes the release artifacts after the gates pass:
   - GoReleaser builds multi-arch binaries + Docker images
   - Cosign signs every published image tag (vX.Y.Z, X.Y) at digest
   - syft generates an SPDX-JSON SBOM
   - cosign attests the SBOM at the same digest
   - SBOM uploaded as a release asset
   - Release is flipped from draft to published (via the App token, so the
     `release.published` event fires)
   - Helm chart is packaged and pushed to `oci://ghcr.io/paperclipinc/charts`
7. **operatorhub-submit.yaml** fires on the published-release event:
   - Validates that the source bundle CSV still has release-ready
     `metadata.name`, `metadata.annotations.containerImage`, and `spec.version`
     fields
   - Forks `k8s-operatorhub/community-operators`, creates a branch with the
     new bundle, opens a PR
   - Same for `redhat-openshift-ecosystem/community-operators-prod`
8. **Conformance suite** also runs on tag pushes
   (`conformance.yaml`'s tag trigger), in addition to the release workflow's
   reusable conformance gate.

## Required gates by change type

Pull requests that change `api/v1/**`, `internal/controller/**`,
`internal/resources/**`, `internal/webhook/**`, `cmd/**`, `config/**`,
`charts/**`, or `bundle/**` must account for the conformance workflow. The
workflow runs on those paths in PRs and on main, and the release workflow calls
the same suite before publishing.

For API, controller, resource builder, webhook, or command changes, run the
relevant unit or envtest target and at least the conformance shard that covers
the behavior. The release gate always reruns `make test-readonly` and the full
conformance workflow.

For distribution changes under `config/**`, `charts/**`, or `bundle/**`, run
the distribution gates that apply to the touched files:

- `make manifests generate` followed by a clean `git diff --exit-code`
- `bash hack/check-helm-rbac.sh`
- `make sync-bundle-rbac-check`
- `make bundle-validate`
- `bash hack/check-distribution-parity.sh` when that script exists

Do not hand-edit generated or release-templated bundle fields to satisfy a
gate. Fix the source that generates the field, then regenerate in the
appropriate integration step.

## Cutting v1.0.0 from v0.1.0

The manifest starts at `0.1.0` (Plan 6 Task 2 explains why). To make
release-please cut a *major* version (`v1.0.0`):

1. Make a commit on main with a `!` to mark a breaking change:
   `feat!: declare v1 API stability`
2. release-please bumps from `0.1.0` directly to `1.0.0`.
3. Merge the release PR as normal.

After v1.0.0, regular `feat:` bumps minor, regular `fix:` bumps patch.

## Manual fallbacks

If the bundle PR didn't open (network blip, fork name collision), trigger
manually:

```bash
gh workflow run "OperatorHub Submission" -f tag=vX.Y.Z
```

If a release was tagged but the release workflow didn't run (very rare:
usually because the PAT expired), retag:

```bash
git tag -d vX.Y.Z
git push origin :refs/tags/vX.Y.Z
git tag vX.Y.Z <sha>
git push origin vX.Y.Z   # uses your local git creds, not GHA's GITHUB_TOKEN
```

## Verifying a release manually

```bash
make verify-signing       # uses gh release view to find the latest tag
cosign verify ghcr.io/paperclipinc/hermes-operator:vX.Y.Z \
  --certificate-identity-regexp 'https://github.com/paperclipinc/hermes-operator/.github/workflows/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
```

See `docs/security/signing.md` for the full verification ritual.

## What ships with each release

- Multi-arch (linux/amd64 + linux/arm64) operator image:
  `ghcr.io/paperclipinc/hermes-operator:vX.Y.Z` and `:X.Y`
- Multi-arch agent image:
  `ghcr.io/paperclipinc/hermes-agent:vX.Y.Z` (built by a separate hermes-agent
  release; the operator's `appVersion` doesn't pin agent versions:
  `spec.image.tag` does)
- OLM bundle image:
  `ghcr.io/paperclipinc/hermes-operator-bundle:vX.Y.Z`
- Helm chart (OCI):
  `oci://ghcr.io/paperclipinc/charts/hermes-operator:X.Y.Z`
- Plain manifests:
  `https://github.com/paperclipinc/hermes-operator/releases/download/vX.Y.Z/install.yaml`
- SBOM:
  `https://github.com/paperclipinc/hermes-operator/releases/download/vX.Y.Z/sbom.spdx.json`
- Cosign signature + SBOM attestation against every image digest
- OperatorHub PRs (auto-opened): community-operators + community-operators-prod

## What does NOT ship

- Source archives (the tag itself is the source-of-truth)
- Pre-built operator binaries outside the Docker image (operator-only use is
  rare; we don't optimise for it)
- Krew plugin (post-v1; see spec section 12)
