#!/usr/bin/env bash
# Install this file root-owned and invoke it only as the forced SSH command.
# The workflow supplies a Docker image archive on stdin; SSH cannot run arbitrary shell.
set -Eeuo pipefail

original_command=${SSH_ORIGINAL_COMMAND:-}
if [[ ! "$original_command" =~ ^deploy\ ([0-9a-f]{40})$ ]]; then
  echo "Only deploy <full-commit-sha> is allowed." >&2
  exit 2
fi
release_sha=${BASH_REMATCH[1]}
config_dir="$HOME/.config/city4christ"
env_file="$config_dir/api.env"
container_name=city4christ-api
previous_name=city4christ-api-previous
image="city4christ-api:$release_sha"

export XDG_RUNTIME_DIR="/run/user/$(id -u)"
export DOCKER_HOST="unix://$XDG_RUNTIME_DIR/docker.sock"
if [[ ! -S "$XDG_RUNTIME_DIR/docker.sock" ]]; then
  echo "Rootless Docker is not running for the deployment user." >&2
  exit 1
fi

if [[ ! -r "$env_file" ]]; then
  echo "Runtime environment file is missing: $env_file" >&2
  exit 1
fi
for required_key in DATABASE_URL VERIFICATION_KEY_BASE64 APP_ORIGIN CORS_ORIGINS; do
  if ! grep -Eq "^${required_key}=.+$" "$env_file"; then
    echo "Required runtime setting is missing or empty: $required_key" >&2
    exit 1
  fi
done

docker load
if ! docker image inspect city4christ:verify >/dev/null 2>&1; then
  echo "Image archive did not contain the smoke-tested city4christ:verify image." >&2
  exit 1
fi
image_platform=$(docker image inspect --format '{{.Os}}/{{.Architecture}}' city4christ:verify)
case "$(uname -m):$image_platform" in
  x86_64:linux/amd64|aarch64:linux/arm64) ;;
  *)
    echo "Image platform $image_platform does not match VPS architecture $(uname -m)." >&2
    exit 1
    ;;
esac
docker image tag city4christ:verify "$image"

# Apply only the repository's forward migrations before replacing the running API.
docker run --rm --env-file "$env_file" -e PORT=10000 "$image" /app/api migrate

had_previous=false
if docker container inspect "$previous_name" >/dev/null 2>&1; then
  docker rm -f "$previous_name" >/dev/null
fi
if docker container inspect "$container_name" >/dev/null 2>&1; then
  docker stop --time 15 "$container_name" >/dev/null
  docker rename "$container_name" "$previous_name"
  had_previous=true
fi

if ! docker run -d \
  --name "$container_name" \
  --restart unless-stopped \
  --publish 127.0.0.1:3000:10000 \
  --env-file "$env_file" \
  --env PORT=10000 \
  --cap-drop ALL \
  --security-opt no-new-privileges \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  --log-opt max-size=10m \
  --log-opt max-file=5 \
  "$image" /app/api; then
  if [[ "$had_previous" == true ]]; then
    docker rename "$previous_name" "$container_name"
    docker start "$container_name" >/dev/null
  fi
  exit 1
fi

healthy=false
for _ in {1..30}; do
  if curl --fail --silent --show-error --max-time 3 http://127.0.0.1:3000/health/ready >/dev/null; then
    healthy=true
    break
  fi
  sleep 2
done

if [[ "$healthy" != true ]]; then
  docker stop --time 10 "$container_name" >/dev/null || true
  if [[ "$had_previous" == true ]]; then
    docker rm "$container_name" >/dev/null
    docker rename "$previous_name" "$container_name"
    docker start "$container_name" >/dev/null
    echo "New API failed readiness; previous container was restored. Inspect it from an administrator session." >&2
  else
    echo "New API failed readiness. Inspect it from an administrator session." >&2
  fi
  exit 1
fi

echo "API deployment $release_sha is ready at http://127.0.0.1:3000."
