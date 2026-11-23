#!/usr/bin/env bash
# Smoke-tests TLS in the Aviary Docker image: HTTPS on first start, a cert
# that persists across restarts, a unique cert per container, no private key
# in the image, user-supplied certs, extra SANs, plain HTTP, and the
# healthcheck in both modes.
#
# Usage: scripts/docker-tls-smoke.sh IMAGE
set -euo pipefail

image="${1:?usage: $0 IMAGE}"
prefix="aviary-tls-smoke-$$"
work="$(mktemp -d)"
containers=()
volumes=()

cleanup() {
  status=$?
  if [[ $status -ne 0 ]]; then
    for c in "${containers[@]}"; do docker logs "$c" 2>&1 | tail -40 || true; done
  fi
  for c in "${containers[@]}"; do docker rm -f "$c" >/dev/null 2>&1 || true; done
  for v in "${volumes[@]}"; do docker volume rm -f "$v" >/dev/null 2>&1 || true; done
  rm -rf "$work"
  exit $status
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

# start NAME PORT [docker run args...] starts a detached container.
start() {
  local name="$prefix-$1" port="$2"
  shift 2
  containers+=("$name")
  docker run -d --name "$name" -p "127.0.0.1:$port:16677" "$@" "$image" >/dev/null
}

# wait_healthy NAME waits for the container's healthcheck to pass.
wait_healthy() {
  local name="$prefix-$1" status
  for _ in $(seq 1 60); do
    status="$(docker inspect -f '{{.State.Health.Status}}' "$name")"
    [[ "$status" == "healthy" ]] && return 0
    sleep 2
  done
  fail "$name never became healthy (last status: $status)"
}

# fingerprint PORT prints the SHA-256 fingerprint of the cert served on PORT.
fingerprint() {
  openssl s_client -connect "127.0.0.1:$1" -servername localhost </dev/null 2>/dev/null |
    openssl x509 -noout -fingerprint -sha256
}

# sans PORT prints the SANs of the cert served on PORT.
sans() {
  openssl s_client -connect "127.0.0.1:$1" -servername localhost </dev/null 2>/dev/null |
    openssl x509 -noout -ext subjectAltName
}

echo "--- the image holds no TLS key"
keys="$(docker run --rm --entrypoint sh "$image" -c \
  'find / -xdev \( -name "*.pem" -o -name "*.key" \) -path "*aviary*" 2>/dev/null; ls -A /home/bot/.config/aviary')"
[[ -z "$keys" ]] || fail "image ships cert/key files: $keys"

echo "--- HTTPS on first start"
vol_a="$prefix-a"
volumes+=("$vol_a")
start a 16701 -v "$vol_a:/home/bot/.config/aviary" -e AVIARY_CONFIG_SERVER_TLS_SANS=aviary.example,203.0.113.7
wait_healthy a
curl -fsk https://127.0.0.1:16701/api/health >/dev/null || fail "no HTTPS on first start"
sans_a="$(sans 16701)"
for want in DNS:localhost DNS:aviary.example "IP Address:127.0.0.1" "IP Address:203.0.113.7"; do
  grep -q "$want" <<<"$sans_a" || fail "cert SANs lack $want: $sans_a"
done
fp_a="$(fingerprint 16701)"

echo "--- the cert persists across restarts"
docker restart "$prefix-a" >/dev/null
wait_healthy a
[[ "$(fingerprint 16701)" == "$fp_a" ]] || fail "cert changed across restart"
docker rm -f "$prefix-a" >/dev/null
start a2 16701 -v "$vol_a:/home/bot/.config/aviary" -e AVIARY_CONFIG_SERVER_TLS_SANS=aviary.example,203.0.113.7
wait_healthy a2
[[ "$(fingerprint 16701)" == "$fp_a" ]] || fail "cert changed for a new container on the same volume"

echo "--- separate containers generate different certs"
start b 16702
wait_healthy b
[[ "$(fingerprint 16702)" != "$fp_a" ]] || fail "two containers share a cert"

echo "--- a mounted cert/key is used"
openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 -nodes -days 1 \
  -subj "/CN=custom.example" -addext "subjectAltName=DNS:custom.example" \
  -keyout "$work/key.pem" -out "$work/cert.pem" 2>/dev/null
chmod 644 "$work/key.pem" "$work/cert.pem"
start c 16703 -v "$work:/certs:ro" \
  -e AVIARY_CONFIG_SERVER_TLS_CERT=/certs/cert.pem -e AVIARY_CONFIG_SERVER_TLS_KEY=/certs/key.pem
wait_healthy c
want_fp="$(openssl x509 -in "$work/cert.pem" -noout -fingerprint -sha256)"
[[ "$(fingerprint 16703)" == "$want_fp" ]] || fail "mounted cert not served"

echo "--- TLS can be disabled"
start d 16704 -e AVIARY_CONFIG_SERVER_NO_TLS=true
wait_healthy d
curl -fs http://127.0.0.1:16704/api/health >/dev/null || fail "no plain HTTP with TLS disabled"

echo "OK: Docker TLS smoke tests passed"
