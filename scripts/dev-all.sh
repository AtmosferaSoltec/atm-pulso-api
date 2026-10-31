#!/usr/bin/env bash
# Levanta el entorno LOCAL completo de Pulso. Nada de esto toca producción.
#
#   scripts/dev-all.sh start    auth-api :3130, storage-api :8084, identity-api :8086,
#                               pulso-api :3001, pulso-web :5173, console-web :5174
#   scripts/dev-all.sh stop     apaga lo que arrancó este script
#   scripts/dev-all.sh status   muestra qué puertos están escuchando
#
# Requiere: Postgres local (atmosfera-postgres :5432 y atm-auth-postgres :5433) y los .env de cada repo.
# storage-api usa el bucket de DESARROLLO de su .env; identity-api consume cuota real de APIs Peru (usar poco).
# documents-api (:8085) no lo gestiona este script: Pulso no lo usa.
# Logs y pids en /tmp/pulso-dev (se pueden borrar).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RUN=/tmp/pulso-dev
mkdir -p "$RUN"

start_one() { # nombre puerto directorio comando...
	local name=$1 port=$2 dir=$3
	shift 3
	if lsof -iTCP:"$port" -sTCP:LISTEN -n -P >/dev/null 2>&1; then
		echo "• $name ya escucha en :$port"
		return
	fi
	(cd "$dir" && nohup "$@" >"$RUN/$name.log" 2>&1 & echo $! >"$RUN/$name.pid")
	echo "• $name arrancando en :$port (log: $RUN/$name.log)"
}

case "${1:-status}" in
start)
	start_one auth-api 3130 "$ROOT/atm-auth-api" \
		env HTTP_ADDR=:3130 CORS_ALLOWED_ORIGINS=http://localhost:5173,http://localhost:5174 go run ./cmd/api
	start_one storage-api 8084 "$ROOT/atm-storage-api" go run ./cmd/api serve
	start_one identity-api 8086 "$ROOT/atm-identity-api" env PORT=8086 go run ./cmd/server
	start_one pulso-api 3001 "$ROOT/atm-pulso-api" ./scripts/dev.sh
	start_one pulso-web 5173 "$ROOT/atm-pulso-web" pnpm dev --port 5173
	start_one console-web 5174 "$ROOT/atm-console-web" \
		env AUTH_API_URL=http://localhost:3130 pnpm dev --port 5174
	;;
stop)
	for pidfile in "$RUN"/*.pid; do
		[ -e "$pidfile" ] || continue
		pid=$(cat "$pidfile")
		# mata el grupo de procesos hijo (go run / pnpm lanzan subprocesos)
		pkill -P "$pid" 2>/dev/null || true
		kill "$pid" 2>/dev/null || true
		rm -f "$pidfile"
	done
	echo "apagado"
	;;
status)
	for p in 3130 8084 8086 3001 5173 5174; do
		if lsof -iTCP:"$p" -sTCP:LISTEN -n -P >/dev/null 2>&1; then echo "✓ :$p"; else echo "✗ :$p"; fi
	done
	;;
*)
	echo "uso: $0 start|stop|status" >&2
	exit 2
	;;
esac
