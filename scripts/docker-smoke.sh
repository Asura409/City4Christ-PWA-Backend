#!/usr/bin/env bash
# Disposable container/database check. Never consumes DATABASE_URL or staging secrets.
set -euo pipefail
image=${1:-city4christ:verify}
suffix="c4c-smoke-$$"
db="${suffix}-db"
api="${suffix}-api"
worker="${suffix}-worker"
cleanup() {
  docker rm -fv "$api" "$worker" "$db" >/dev/null 2>&1 || true
  docker network rm "$suffix" >/dev/null 2>&1 || true
}
trap cleanup EXIT
trap 'exit 130' INT TERM
docker network create "$suffix" >/dev/null
docker run -d --name "$db" --network "$suffix" -e POSTGRES_USER=smoke -e POSTGRES_PASSWORD=disposable-only -e POSTGRES_DB=smoke postgres:16-bookworm >/dev/null
for attempt in {1..60}; do
  if docker exec "$db" pg_isready -h 127.0.0.1 -U smoke -d smoke >/dev/null 2>&1; then break; fi
  sleep 1
done
docker exec "$db" pg_isready -h 127.0.0.1 -U smoke -d smoke >/dev/null
# Fixed non-secret key used exclusively in this disposable test.
key=AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=
url="postgres://smoke:disposable-only@${db}:5432/smoke?sslmode=disable"
docker run --rm --network "$suffix" -e DATABASE_URL="$url" -e VERIFICATION_KEY_BASE64="$key" "$image" /app/api migrate
docker exec "$db" psql -U smoke -d smoke -v ON_ERROR_STOP=1 -c "INSERT INTO accounts(email,first_name,last_name) VALUES ('preserve@example.org','Smoke','Test')" >/dev/null
docker run --rm --network "$suffix" -e DATABASE_URL="$url" -e VERIFICATION_KEY_BASE64="$key" "$image" /app/api migrate
test "$(docker exec "$db" psql -U smoke -d smoke -tAc "SELECT count(*) FROM accounts WHERE email='preserve@example.org'")" = 1
docker run -d --name "$api" --network "$suffix" -p 127.0.0.1::10000 -e DATABASE_URL="$url" -e VERIFICATION_KEY_BASE64="$key" -e APP_ORIGIN=https://smoke.example "$image" /app/api migrate-and-serve >/dev/null
endpoint="http://$(docker port "$api" 10000/tcp)"
for attempt in {1..60}; do
  if curl --fail --silent "$endpoint/health/ready" >/dev/null; then break; fi
  sleep 1
done
curl --fail --silent "$endpoint/health/ready"
curl --fail --silent "$endpoint/docs/" >/dev/null
curl --fail --silent "$endpoint/openapi.json" >/dev/null
curl --fail --silent "$endpoint/api/v1/sermons" >/dev/null
curl --fail --silent "$endpoint/docs/swagger-ui-bundle.js" >/dev/null
test "$(curl --silent -o /dev/null -w '%{http_code}' -H 'Content-Type: application/json' -d '{}' "$endpoint/api/v1/auth/register")" = 403
test "$(curl --silent -o /dev/null -w '%{http_code}' -H 'Origin: https://smoke.example' -H 'Content-Type: application/json' -d '{}' "$endpoint/api/v1/admin/sermons")" = 401
test "$(docker inspect -f '{{.Config.User}}' "$api")" = 65532:65532
docker run -d --name "$worker" "$image" /app/email-worker >/dev/null
sleep 2
test "$(docker inspect -f '{{.State.Running}}' "$worker")" = true
docker stop --time 15 "$api" "$worker" >/dev/null
test "$(docker inspect -f '{{.State.ExitCode}}' "$api")" = 0
test "$(docker inspect -f '{{.State.ExitCode}}' "$worker")" = 0
echo 'Docker smoke checks passed with disposable PostgreSQL.'
