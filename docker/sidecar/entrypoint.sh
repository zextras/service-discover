#!/bin/sh
TOKEN_FILE="${TOKEN_FILE:-/etc/carbonio/${SERVICE_NAME}/service-discover/token}"
export SETUP_CONSUL_TOKEN="${SETUP_CONSUL_TOKEN:-$CONSUL_HTTP_TOKEN}"

is_mesh_service() {
  ls /consul/config/*.hcl >/dev/null 2>&1
}

do_setup() {
  echo "[sidecar] Waiting for consul agent..."
  until consul members >/dev/null 2>&1; do sleep 1; done
  echo "[sidecar] Consul agent ready."
  if is_mesh_service; then
    consul services register /consul/config/*.hcl
  fi
  if [ -n "${SETUP_SCRIPT}" ]; then
    (${SETUP_SCRIPT} || true)
  fi
  [ -d /shared-tokens ] && [ -s "$TOKEN_FILE" ] && cp "$TOKEN_FILE" /shared-tokens/token \
    && chmod 0644 /shared-tokens/token
}

do_start() {
  if ! is_mesh_service; then
    echo "[sidecar] ${SERVICE_NAME} is not a mesh service, no envoy to start. Idling."
    exec sleep infinity
  fi
  echo "[sidecar] Starting envoy for ${SERVICE_NAME}"
  exec consul connect envoy \
    -sidecar-for="${SERVICE_NAME}" \
    -admin-bind=localhost:0 \
    -token-file="$TOKEN_FILE" \
    ${ENVOY_EXTRA_ARGS:-}
}

case "${1:-all}" in
  setup)
    do_setup
    ;;
  start)
    do_start
    ;;
  *)
    do_setup
    do_start
    ;;
esac
