#!/bin/sh
set -eu

mkdir -p \
  /data/var/uploads \
  /data/var/cache

cd /data
exec /opt/superflare/superflare "$@"
