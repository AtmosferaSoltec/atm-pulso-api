#!/usr/bin/env bash
# Levanta el entorno LOCAL completo de Pulso. Nada de esto toca producción.
#
#   scripts/dev-all.sh start    auth-api :3130, storage-api :8084, identity-api :8086,
#                               pulso-api :3001, pulso-web :5173, console-web :5174
#   scripts/dev-all.sh stop     apaga lo que arrancó este script
#   scripts/dev-all.sh status   muestra qué puertos están escuchando
#
#   LAN=1 scripts/dev-all.sh start   DEMO EN RED LOCAL: igual, pero accesible desde otro dispositivo de la misma
#                               red Wi-Fi (p. ej. una tablet). Detecta la IP de esta Mac (o usa LAN_IP=x.x.x.x),
#                               levanta vite con --host, apunta pulso-web a pulso-api y auth-api por esa IP y amplía
#                               CORS con ese origen. Todo con variables de entorno al lanzar: NINGÚN .env cambia.
#                               Si un servicio ya escuchaba en modo normal, haz `stop` y vuelve a `start`.
#                               No abre túneles ni nada hacia internet; solo la red local.
#
# Requiere: Postgres local (atmosfera-postgres :5432 y atm-auth-postgres :5433) y los .env de cada repo.
# storage-api usa el bucket de DESARROLLO de su .env; identity-api consume cuota real de APIs Peru (usar poco).
# documents-api (:8085) no lo gestiona este script: Pulso no lo usa.
# Logs y pids en /tmp/pulso-dev (se pueden borrar).
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
RUN=/tmp/pulso-dev
mkdir -p "$RUN"

# --- Modo LAN -------------------------------------------------------------------------------------------------
lan_ip() {
	local ip="${LAN_IP:-}" iface
	if [ -z "$ip" ]; then
		for iface in en0 en1 en2; do
			ip=$(ipconfig getifaddr "$iface" 2>/dev/null || true)
			[ -n "$ip" ] && break
		done
	fi
	if [ -z "$ip" ]; then
		echo "LAN=1: no pude detectar la IP de la red local (¿Wi-Fi apagado?). Indícala con LAN_IP=x.x.x.x" >&2
		exit 1
	fi
	echo "$ip"
}

ORIGINS=http://localhost:5173,http://localhost:5174
WEB_ARGS=(--port 5173)
CONSOLE_ARGS=(--port 5174)
WEB_ENV=()
if [ "${LAN:-}" = "1" ] && [ "${1:-status}" = "start" ]; then
	IP=$(lan_ip)
	ORIGINS="$ORIGINS,http://$IP:5173,http://$IP:5174"
	WEB_ARGS=(--host 0.0.0.0 --port 5173 --strictPort)
	CONSOLE_ARGS=(--host 0.0.0.0 --port 5174 --strictPort)
	WEB_ENV=(VITE_API_URL="http://$IP:3001/api/v1" VITE_AUTH_API_URL="http://$IP:3130")
fi

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
		env HTTP_ADDR=:3130 CORS_ALLOWED_ORIGINS="$ORIGINS" go run ./cmd/api
	start_one storage-api 8084 "$ROOT/atm-storage-api" go run ./cmd/api serve
	start_one identity-api 8086 "$ROOT/atm-identity-api" env PORT=8086 go run ./cmd/server
	# CORS_ORIGIN de .env solo se sustituye en modo LAN (dev.sh no pisa lo que ya está en el entorno).
	if [ "${LAN:-}" = "1" ]; then
		start_one pulso-api 3001 "$ROOT/atm-pulso-api" env CORS_ORIGIN="$ORIGINS" ./scripts/dev.sh
	else
		start_one pulso-api 3001 "$ROOT/atm-pulso-api" ./scripts/dev.sh
	fi
	start_one pulso-web 5173 "$ROOT/atm-pulso-web" env ${WEB_ENV[@]+"${WEB_ENV[@]}"} pnpm dev "${WEB_ARGS[@]}"
	start_one console-web 5174 "$ROOT/atm-console-web" \
		env AUTH_API_URL=http://localhost:3130 pnpm dev "${CONSOLE_ARGS[@]}"
	if [ "${LAN:-}" = "1" ]; then
		echo
		echo "Demo en red local (la tablet y esta Mac deben estar en la misma red Wi-Fi):"
		echo "  Web Pulso :  http://$IP:5173/        Quiosco: http://$IP:5173/kiosk"
		echo "  Consola   :  http://$IP:5174/"
		echo "  Pulso API :  http://$IP:3001/api/v1/health    Auth API: http://$IP:3130"
		echo "  Si no abre, revisa el firewall de macOS (permite conexiones entrantes a node y api)."
	fi
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
