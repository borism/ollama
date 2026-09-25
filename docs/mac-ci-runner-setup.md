# One-time setup: Tart golden image for `darwin-build`

Replaces GitHub's paid `macos-26-xlarge` runner with an ephemeral macOS VM
on your own Mac (via [Tart](https://github.com/openai/tart), built on
Apple's Virtualization.framework), so `darwin-build` doesn't cost anything
and doesn't need GitHub billing sorted out.

**Over SSH, the external volume is blocked by default** (`Operation not
permitted` on anything under `/Volumes/T9`, even though `df` works) — macOS
privacy controls don't cover remote sessions unless you opt in. Rebooting
doesn't fix it, and granting Full Disk Access to `sshd` doesn't either. The
fix: System Settings → General → Sharing → Remote Login (ⓘ) → **Allow full
disk access for remote users** (or add `/usr/libexec/sshd-keygen-wrapper` under
Privacy & Security → Full Disk Access). New SSH sessions pick it up
immediately. Local Terminal.app is unaffected either way, and step 3 (Setup
Assistant) needs the graphical session regardless.

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

Download it from <https://github.com/openai/tart/releases/latest>
(`tart.tar.gz`, verify against the release's checksums file).

```shell
mkdir -p /Volumes/T9/tart-home/bin
mv tart.app /Volumes/T9/tart-home/bin/   # from wherever you extracted it
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
machine. **Skip the Apple ID sign-in** — it fails inside the VM (correct
password rejected), and Recovery's password reset can't work either because
it needs that same sign-in to "deactivate" the Mac. So **write the account
password down**: forgetting it means recreating the VM. Then in System
Settings on the guest:

- **Users & Groups → Login Options → Automatic login** — set to that
  account. The ephemeral runner script needs a logged-in session waiting,
  not a login screen, every time it clones and boots this image (the guest
  agent from step 5 runs inside that session).
- **General → Sharing → Remote Login** — turn on, and in its (ⓘ) panel turn
  on **Allow full disk access for remote users**. Only needed for setup over
  SSH (`ssh <user>@$(tart ip ollama-cluster-darwin-golden)` from the host);
  the runner itself uses `tart exec`.

## 4. Inside the guest: install the build toolchain

Xcode needs an Apple ID to download, which the guest can't do, so download
the Xcode `.xip` **on the host** from
<https://developer.apple.com/download/all/> and copy it in (`scp` from the
host to the guest's IP). Xcode must support the guest's macOS version —
Xcode 27 on a macOS 27 guest, for example; `tart create --from-ipsw=latest`
picks the newest macOS the host supports.

Install Homebrew **first**: its installer pulls in the Command Line Tools and
switches `xcode-select` to them, which would undo the Xcode selection below
and hide the Metal compiler.

```shell
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.bash_profile   # tart exec runs bash -l
echo 'eval "$(/opt/homebrew/bin/brew shellenv)"' >> ~/.zprofile
eval "$(/opt/homebrew/bin/brew shellenv)"
brew install cmake go ccache
```

Then Xcode (`darwin-build` needs the Metal toolchain, which the Command Line
Tools alone don't have):

```shell
xip --expand ~/Xcode_*.xip && mv Xcode*.app /Applications/Xcode.app && rm ~/Xcode_*.xip
sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
sudo xcodebuild -license accept
sudo xcodebuild -runFirstLaunch
xcodebuild -downloadComponent MetalToolchain
xcrun --find metal     # must print a path
xcodebuild -version    # note it for troubleshooting later
```

## 5. Install the Tart Guest Agent (needed for `tart exec`)

The `cirruslabs/cli` Homebrew tap still has a formula, but it's stuck at an
old version; the agent now lives at `openai/tart-guest-agent`. Install the
release binary and run it as a launchd agent in the logged-in session (the
plist is `cirruslabs/macos-image-templates`' `data/tart-guest-agent.plist`,
with the working directory changed to your account's home):

```shell
V=0.15.0   # latest from https://github.com/openai/tart-guest-agent/releases
B=https://github.com/openai/tart-guest-agent/releases/download/v$V
cd "$(mktemp -d)"
curl -fsSLO $B/tart-guest-agent-darwin-all.tar.gz
curl -fsSLO $B/tart-guest-agent_${V}_checksums.txt
grep " tart-guest-agent-darwin-all.tar.gz$" tart-guest-agent_${V}_checksums.txt | shasum -a 256 -c
tar xzf tart-guest-agent-darwin-all.tar.gz
install -m 755 tart-guest-agent /opt/homebrew/bin/

cat > org.cirruslabs.tart-guest-agent.plist <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
    <dict>
        <key>Label</key>
        <string>org.cirruslabs.tart-guest-agent</string>
        <key>ProgramArguments</key>
        <array>
            <string>/opt/homebrew/bin/tart-guest-agent</string>
            <string>--run-agent</string>
        </array>
        <key>EnvironmentVariables</key>
        <dict>
            <key>PATH</key>
            <string>/bin:/usr/bin:/usr/sbin:/usr/local/bin:/opt/homebrew/bin</string>
            <key>TERM</key>
            <string>xterm-256color</string>
        </dict>
        <key>WorkingDirectory</key>
        <string>$HOME</string>
        <key>RunAtLoad</key>
        <true/>
        <key>KeepAlive</key>
        <true/>
        <key>StandardOutPath</key>
        <string>/tmp/tart-guest-agent.log</string>
        <key>StandardErrorPath</key>
        <string>/tmp/tart-guest-agent.log</string>
    </dict>
</plist>
EOF
sudo install -o root -g wheel -m 644 org.cirruslabs.tart-guest-agent.plist /Library/LaunchAgents/
launchctl bootstrap gui/$(id -u) /Library/LaunchAgents/org.cirruslabs.tart-guest-agent.plist
```

Check from the host: `tart exec ollama-cluster-darwin-golden bash -lc "xcrun
--find metal; go version"`. Note: no `--` before the command — `tart exec`
passes it through to the guest literally. A `failed to run vdagent` line in
`/tmp/tart-guest-agent.log` is harmless (clipboard sharing, unused here).

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
