# Releasing (this fork)

How `.github/workflows/release.yaml` and `scripts/install.sh` work for
`borism/ollama-cluster`, and what's deliberately not built yet.

## Scope

- **Linux**: amd64 + arm64, **the same GPU backends as stock Ollama**,
  built from the same `Dockerfile` stages upstream's release uses, one
  stage per job (`linux-depends`) on GitHub-hosted `ubuntu-latest` /
  `ubuntu-24.04-arm`, then assembled and split per arch (`linux-build`)
  the way `scripts/build_linux.sh` does:
  - amd64: CPU, CUDA 12, CUDA 13, Vulkan; ROCm 7.2 as a separate extra.
  - arm64: CPU, CUDA 12, CUDA 13; JetPack 5 and 6 as separate extras.

  **No MLX engine on Linux** (upstream builds `mlx_cuda_v13` with a
  200 GB swap file; a hosted runner can't). No registry layer cache
  either, so every release rebuilds every backend: the slowest job
  (amd64 CUDA 12, 11 GPU architectures) took 2h42m of the 6h job limit
  on the first run. Upgrade path: `cache-from`/`cache-to` against a GHCR
  registry ref, same shape as upstream's Docker Hub cache.
- **macOS**: universal (amd64+arm64) binary, **unsigned**. Built via
  `scripts/build_darwin.sh build package` -- deliberately skips `sign`
  (needs an Apple Developer ID + notarization secrets this fork doesn't
  have) and `app` (the signed menu-bar .app; needs npm, and Gatekeeper
  would refuse it unsigned anyway regardless). Runs on a **self-hosted**
  ephemeral Tart VM on personal hardware, not GitHub's paid
  `macos-26-xlarge` runner -- see `docs/mac-ci-runner-setup.md` for the
  one-time setup and the security scoping this requires (this repo is
  public).
- **No Windows.** Upstream's `windows-depends`/`windows-build`/
  `windows-app` jobs (384 lines) needed a Windows Authenticode cert and
  Google KMS signing credentials; dropped rather than left broken.
- **No Docker images.** Upstream's `docker-build-push`/
  `docker-merge-push` jobs needed a Docker Hub account to push to;
  separate concern from the curl\|sh binary release anyway.

Upgrading any of these is additive (new matrix entries / new jobs), not
a rewrite -- see the `# ponytail:` comments in release.yaml at each cut
corner for the specific upgrade path.

## Release artifacts

Both platforms produce the same `bin/ollama` + `lib/ollama/*` layout
(`ml/path.go`'s runtime lookup accepts this on macOS too, not just
Linux):

- `ollama-linux-amd64.tar.zst`, `ollama-linux-arm64.tar.zst` -- CPU,
  CUDA, Vulkan
- `ollama-linux-amd64-rocm.tar.zst`, `ollama-linux-arm64-jetpack5.tar.zst`,
  `ollama-linux-arm64-jetpack6.tar.zst` -- extras, extracted over the
  main tarball
- `ollama-darwin.tgz` (universal)

`scripts/install.sh` is upstream's script with only three changes
(documented at its top): it downloads from
`https://github.com/borism/ollama-cluster/releases/...` instead of
`ollama.com`, `OLLAMA_VERSION` picks a release tag
(`releases/download/vX.Y.Z/...`, otherwise `releases/latest/download/...`),
and on macOS it installs the CLI tarball instead of `Ollama.app`.
Everything else -- detecting NVIDIA/AMD/Jetson hardware, fetching the
matching extra, setting up NVIDIA drivers and the systemd service -- is
upstream's, unchanged. `releases/latest` never resolves to a
pre-release, so the release job creates a normal (non-pre-release)
draft for the plain `curl | sh` to find once published.

## Cutting a release

```shell
git tag vX.Y.Z
git push origin vX.Y.Z
```

The `release` job (gated to actual tag pushes, see below) publishes a
**draft** GitHub Release -- review and publish it manually once the
artifacts look right. Tag as `v<upstream version>-cluster.N` (e.g.
`v0.34.4-cluster.1`): the tag becomes `ollama --version`, and the
ollama.com registry refuses pulls from clients reporting a version older
than the models need (HTTP 412), so a fresh `v0.0.1` can't pull current
models.

## Testing the workflow without cutting a real release

Two real constraints hit while building this, worth knowing before you
try:

- **`workflow_dispatch` only sees workflow files on the default branch
  (`main`).** Run it with `gh workflow run release.yaml --ref <branch>
  -f version=0.0.0-test` to build without publishing: `release`'s own
  tag-only guard (`if: startsWith(github.ref, 'refs/tags/')`) skips the
  publish step, because there `GITHUB_REF_NAME` is a branch name, not a
  version.
- **GitHub disables all Actions workflows on a fresh fork by default**,
  separately from the repo's Actions permissions settings (which can
  say "enabled" while this is still blocking) -- both `gh api
  .../actions/workflows` and `.../actions/runs` return empty until
  someone visits the repo's Actions tab once and clicks "I understand
  my workflows, go ahead and enable them." No API for it.

Practical loop: push a throwaway tag (`vX.Y.Z-test`), watch the run
(`gh run view <id> --repo borism/ollama-cluster`), delete the tag after
(`git push origin :refs/tags/vX.Y.Z-test`) -- no GitHub Release gets
created unless every job the `release` job `needs:` actually succeeds,
so a partial failure leaves nothing to clean up beyond the tag itself.

## Known gaps found by actually running this

- Originally ran `darwin-build` on GitHub's `macos-26-xlarge` -- a paid
  large runner that fails immediately with a billing error if the
  account's spending limit or payment method isn't in order, and not
  something a workflow can detect or work around. Replaced with a
  self-hosted ephemeral Tart VM (`docs/mac-ci-runner-setup.md`) instead
  of chasing the billing issue.
- `cmake/local.cmake`'s `ollama-go` target bakes its own `-ldflags` from
  the CMake variable `OLLAMA_VERSION` -- it does **not** read the
  `GOFLAGS` env var the way a plain `go build` would. Any new build
  step that invokes `cmake --target ollama-local` (or `ollama-go`)
  directly needs `-DOLLAMA_VERSION="$VERSION"` passed explicitly, the
  same way `build_darwin.sh` already does it internally.
- The pre-Dockerfile arm64 build needed `-DGGML_CPU_ALL_VARIANTS=OFF`
  because `ubuntu-24.04-arm`'s host gcc rejects the `armv9.2_2` (SME)
  CPU variant llama.cpp registers without a compiler check. The
  Dockerfile's own toolchain accepts it, so arm64 releases now ship the
  full set of CPU variants like upstream.
