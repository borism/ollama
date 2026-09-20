# Releasing (this fork)

How `.github/workflows/release.yaml` and `scripts/install.sh` work for
`borism/ollama-cluster`, and what's deliberately not built yet.

## Scope

- **Linux**: amd64 + arm64, **CPU only**. No CUDA/ROCm/Vulkan backend
  builds yet -- those need the same per-backend SDK containers
  `.github/workflows/test.yaml`'s own `linux` matrix already uses
  (`nvidia/cuda:...`, `rocm/dev-ubuntu-...`, a Vulkan SDK apt repo);
  adding a backend here means adding a matching matrix entry there,
  still on GitHub-hosted `ubuntu-latest`/`ubuntu-24.04-arm` (a Docker
  `container:` works fine on a hosted runner for a compile-only build --
  no GPU hardware needed to build against an SDK).
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
Linux), so `scripts/install.sh` needs only one download+extract routine
for both OSes:

- `ollama-linux-amd64.tgz`, `ollama-linux-arm64.tgz`
- `ollama-darwin.tgz` (universal)

`install.sh` points at `https://github.com/borism/ollama-cluster/releases/...`,
not `ollama.com` -- `OLLAMA_VERSION` picks a specific tag
(`releases/download/vX.Y.Z/...`), otherwise it uses
`releases/latest/download/...`.

## Cutting a release

```shell
git tag vX.Y.Z
git push origin vX.Y.Z
```

The `release` job (gated to actual tag pushes, see below) publishes a
**draft, prerelease** GitHub Release -- review and publish it manually
once the artifacts look right.

## Testing the workflow without cutting a real release

Two real constraints hit while building this, worth knowing before you
try:

- **`workflow_dispatch` only works once the triggering workflow file
  exists on the repo's default branch (`main`).** A feature branch
  alone isn't enough, even with `--ref`. Until `ollama-cluster` merges
  to `main`, the only way to actually run this workflow is a real tag
  push -- `release`'s own tag-only guard (`if:
  startsWith(github.ref, 'refs/tags/')`) exists because
  `workflow_dispatch`'s `GITHUB_REF_NAME` is a branch name, not a
  version, so the publish step would misbehave if it ran there anyway.
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
- `ggml_add_cpu_backend_variant(armv9.2_2 ... SME)` (llama.cpp's own
  `ggml/src/CMakeLists.txt`) registers an SME CPU-dispatch variant with
  no compiler-capability check, unlike the PowerPC path in the same
  file. `ubuntu-24.04-arm`'s gcc doesn't accept `+sme`, failing the
  whole arm64 build. Worked around here with
  `-DGGML_CPU_ALL_VARIANTS=OFF` on arm64 only (single baseline variant,
  no runtime CPU dispatch) -- an upstream llama.cpp gap, not something
  to patch in this fork's build script.
