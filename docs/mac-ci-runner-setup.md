# One-time setup: Tart golden image for `darwin-build`

Replaces GitHub's paid `macos-26-xlarge` runner with an ephemeral macOS VM
on your own Mac (via [Tart](https://github.com/openai/tart), built on
Apple's Virtualization.framework), so `darwin-build` doesn't cost anything
and doesn't need GitHub billing sorted out.

**Do all of this locally in Terminal.app on the Mac** (not over SSH — the
`sshd` process needs Full Disk Access to write to an external volume and
that grant has been flaky to get to actually apply without a reboot; local
Terminal.app isn't affected. macOS's first-boot Setup Assistant also needs
a graphical session regardless).

## Why this Mac, why an external SSD

Apple Silicon is required (Virtualization.framework macOS guests are
arm64-only) — this cross-compiles both `amd64`+`arm64` from one host, the
same way GitHub's own `macos-26-xlarge` (also Apple Silicon) does today.
The VM images live on an external SSD (`/Volumes/T9`, case-sensitive APFS —
required for Tart's copy-on-write cloning) because the internal disk didn't
have enough free space for a macOS+Xcode image (needs 80-100GB+; check with
`df -h /Volumes/T9` before starting, and again after step 2 completes).

## Security: read this before registering anything

This repo is **public**. A self-hosted runner executes whatever a workflow
tells it to. The `tart-ephemeral` label (used below and in
`release.yaml`) must **only** ever be attached to a job gated by an actual
tag push or `workflow_dispatch` (both require push access to the repo —
i.e., only you). If a future workflow change ever attaches this label to a
`pull_request`- or `pull_request_target`-triggered job, anyone who opens a
PR from a fork could run arbitrary code on this Mac. Don't do that.

## 1. Install Tart onto the external SSD

A verified copy may already be sitting in `/tmp/tart-install` from an
earlier session (checksum-verified against `openai/tart`'s GitHub release).
If not, download it fresh from
<https://github.com/openai/tart/releases/latest> (`tart.tar.gz`, verify
against the release's checksums file).

```shell
mkdir -p /Volumes/T9/tart-home/bin
mv /tmp/tart-install/tart.app /Volumes/T9/tart-home/bin/   # or wherever you extracted it
export TART_HOME=/Volumes/T9/tart-home
alias tart="$TART_HOME/bin/tart.app/Contents/MacOS/tart"
tart --version
```

Add `export TART_HOME=/Volumes/T9/tart-home` to your shell profile
(`~/.bash_profile` — Terminal.app runs bash as a login shell by default,
not `~/.bashrc`) so it's always set — every `tart` command below, and
`scripts/mac-ci/ephemeral-darwin-runner.sh`, assumes it.

## 2. Create the base VM from a fresh macOS IPSW

```shell
tart create ollama-cluster-darwin-golden --from-ipsw=latest --disk-size 100
```

Downloads a full macOS installer (multi-GB) and installs it into a new VM
disk on T9. Takes a while; let it finish.

## 3. First boot: click through macOS Setup Assistant

```shell
tart run ollama-cluster-darwin-golden
```

A window opens showing the new "Mac"'s setup screen, like unboxing a real
machine. Create a local account, then in System Settings on the guest:

- **Users & Groups → Login Options → Automatic login** — set to that
  account. The ephemeral runner script needs a logged-in session waiting,
  not a login screen, every time it clones and boots this image.
- **General → Sharing → Remote Login** — turn on. `tart exec` (used by the
  wrapper script) needs this.

## 4. Inside the guest: install the build toolchain

Open Terminal inside the guest VM window.

```shell
xcode-select --install
```

Click through the GUI installer that pops up for Command Line Tools. That
alone isn't enough, though — `darwin-build` needs the Metal toolchain
(`xcrun --find metal`), which needs full Xcode: install it from the Mac App
Store inside the guest, or download it from
<https://developer.apple.com/download/all/> (needs an Apple ID either way).
Then:

```shell
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
xcodebuild -downloadComponent MetalToolchain
xcodebuild -version   # note the Xcode version -- not required to be pinned
                       # anywhere (this is a single dedicated machine, not
                       # a shared image with several Xcodes side by side),
                       # but useful to know for troubleshooting later
```

Install Homebrew, then the rest of the build toolchain:

```shell
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
brew install cmake go ccache
```

## 5. Install the Tart Guest Agent (needed for `tart exec`)

```shell
brew install cirruslabs/cli/tart-guest-agent
```

If that tap is also gone (the `cirruslabs` GitHub org's packages appear to
have moved after an ownership change — its `ghcr.io` images returned 404
when checked during setup), check
<https://github.com/orgs/openai/repositories?q=tart> or the current Tart
README for wherever the guest agent lives now before falling back to a
from-source build.

## 6. Install the GitHub Actions runner software — don't register yet

```shell
mkdir ~/actions-runner && cd ~/actions-runner
curl -o actions-runner.tar.gz -L \
    https://github.com/actions/runner/releases/download/vX.XXX.X/actions-runner-osx-arm64-X.XXX.X.tar.gz
tar xzf actions-runner.tar.gz
```

Check <https://github.com/actions/runner/releases> for the current
version/filename. **Don't run `./config.sh` here** — the wrapper script
(`scripts/mac-ci/ephemeral-darwin-runner.sh`) registers fresh on every
clone with a short-lived token it fetches itself, so this golden image
should stay unregistered. Baking in a registration would tie the image to
one token that expires and can't be reused across clones anyway.

## 7. Shut down cleanly — this is now the golden image

```shell
shutdown -h now
```

Back on the host:

```shell
tart list
```

`ollama-cluster-darwin-golden` should show stopped. **Never boot this one
directly for a real release** — `scripts/mac-ci/ephemeral-darwin-runner.sh`
clones it fresh each time (copy-on-write, so this doesn't consume much
extra space per run) and destroys the clone after.

## Using it

Once this is done, cutting a release is: run
`scripts/mac-ci/ephemeral-darwin-runner.sh` locally on the Mac, then (from
wherever) `git push origin vX.Y.Z`. The script blocks until the job
completes, cleans up after itself either way (including on Ctrl-C).

## Rebuilding the golden image later

Toolchain updates (new Xcode, new cmake/go) mean redoing steps 2-7 against
a new VM name, then updating `GOLDEN_IMAGE` in
`scripts/mac-ci/ephemeral-darwin-runner.sh`. Keep the old one around
(`tart list`) until the new one's proven, then `tart delete` it.
