#!/usr/bin/env bash
# Runs the API on this machine with the variables of .env (values may contain
# spaces or < >, so they are exported line by line instead of with `source`).
#
#   scripts/dev.sh                 start the API
#   scripts/dev.sh migrate up      any subcommand of cmd/api
set -euo pipefail
cd "$(dirname "$0")/.."

while IFS= read -r line || [ -n "$line" ]; do
	case "$line" in '' | '#'*) continue ;; esac
	export "${line?}"
done < .env

exec go run ./cmd/api "$@"
