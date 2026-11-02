#!/bin/sh
# Copy a built ./monomodule and hack.json to a Push 3 for a manual test.
# Usage: scripts/deploy.sh [push.local]
# The config (monomodule-config.json) and data/ folder on the device are
# left alone.
set -e
HOST="${1:-push.local}"
DIR=/data/push-hack/hacks/monomodule
[ -x ./monomodule ] || { echo "run 'make' first"; exit 1; }
ssh root@"$HOST" "/etc/init.d/push-hack-monomodule stop" || true
ssh ableton@"$HOST" "mkdir -p $DIR"
scp monomodule hack.json ableton@"$HOST":$DIR/
ssh ableton@"$HOST" "chmod +x $DIR/monomodule"
ssh root@"$HOST" "/etc/init.d/push-hack-monomodule start" || echo "service not installed yet: install via the catalog once, then redeploy"
echo "log: /data/push-hack/logs/monomodule.log"
