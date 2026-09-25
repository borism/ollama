#!/bin/sh
# Run this LOCALLY on the Mac hosting the Tart golden image, right before
# pushing a release tag. Clones the golden image (see
# docs/mac-ci-runner-setup.md), boots it, registers a fresh ephemeral
# GitHub Actions runner inside it (a short-lived registration token fetched
# per run -- nothing long-lived is baked into the image), waits for it to
# pick up and finish exactly one job, then destroys the clone. Ctrl-C is
# safe at any point -- cleanup still runs.
#
# ponytail: single-job, single-machine, run-when-you-need-it. No daemon, no
# queue, no auto-retry -- if darwin-build needs a second attempt, run this
# again. Upgrade path if releases become frequent: a launchd job that polls
# for a queued darwin-build job and runs this automatically.
#
# Security: the golden image + this script are the ONLY thing that should
# ever be able to run as this runner's label (see release.yaml's
# darwin-build `runs-on:` and the comment there) -- this repo is public,
# and a self-hosted runner reachable by a pull_request-triggered job would
# let anyone with a fork run arbitrary code on this Mac. Never attach the
# "tart-ephemeral" label to anything but a tag-push-gated job.

set -eu

export TART_HOME="${TART_HOME:-/Volumes/T9/tart-home}"
TART="${TART_HOME}/bin/tart.app/Contents/MacOS/tart"
GOLDEN_IMAGE="ollama-cluster-darwin-golden"
CLONE_NAME="ollama-cluster-darwin-ephemeral-$$"
REPO="borism/ollama-cluster"

if [ ! -x "$TART" ]; then
    echo "ERROR: tart not found at $TART (check TART_HOME)" >&2
    exit 1
fi

if ! "$TART" list --quiet | grep -qx "${GOLDEN_IMAGE}"; then
    echo "ERROR: golden image '${GOLDEN_IMAGE}' not found -- see docs/mac-ci-runner-setup.md" >&2
    exit 1
fi

cleanup() {
    echo ">>> Cleaning up ${CLONE_NAME}"
    "$TART" stop "${CLONE_NAME}" >/dev/null 2>&1 || true
    "$TART" delete "${CLONE_NAME}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

echo ">>> Cloning ${GOLDEN_IMAGE} -> ${CLONE_NAME}"
"$TART" clone "${GOLDEN_IMAGE}" "${CLONE_NAME}"

echo ">>> Booting ${CLONE_NAME} (no graphics)"
"$TART" run "${CLONE_NAME}" --no-graphics >/tmp/"${CLONE_NAME}".log 2>&1 &

# Wait for the guest agent (not just an IP): tart exec needs the logged-in
# session's agent up, which lands a bit after networking.
echo ">>> Waiting for the guest agent"
i=0
until "$TART" exec "${CLONE_NAME}" true >/dev/null 2>&1; do
    i=$((i + 1))
    if [ "$i" -ge 90 ]; then
        echo "ERROR: timed out waiting for ${CLONE_NAME}'s guest agent -- see /tmp/${CLONE_NAME}.log" >&2
        exit 1
    fi
    sleep 2
done

# REG_TOKEN can come from anywhere with admin on the repo (another box's gh,
# or Settings -> Actions -> Runners -> New runner); gh here is only the fallback.
echo ">>> Runner registration token (expires in ~1 hour, single-use registration)"
REG_TOKEN=${REG_TOKEN:-$(gh api "repos/${REPO}/actions/runners/registration-token" --method POST --jq .token)}

echo ">>> Registering + running the ephemeral runner inside the guest (blocks until it picks up and finishes one job)"
"$TART" exec "${CLONE_NAME}" bash -lc "
    set -eu
    cd ~/actions-runner
    ./config.sh --url 'https://github.com/${REPO}' --token '${REG_TOKEN}' \
        --labels tart-ephemeral,macOS --name '${CLONE_NAME}' --ephemeral --unattended
    ./run.sh
"

echo ">>> Job finished."
