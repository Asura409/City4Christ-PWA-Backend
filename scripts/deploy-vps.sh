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
media_env_file="$config_dir/media-worker.env"
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

# The native media worker is opt-in. It uses the VPS ClamAV Unix socket and is
# installed only after an operator has configured its separate private env file.
if [[ -r "$media_env_file" ]] && grep -Eq '^MEDIA_SCAN_ENABLED=true$' "$media_env_file"; then
  for required_key in DATABASE_URL R2_ACCOUNT_ID R2_BUCKET MEDIA_R2_ACCESS_KEY_ID MEDIA_R2_SECRET_ACCESS_KEY; do
    if ! grep -Eq "^${required_key}=.+$" "$media_env_file"; then
      echo "Required media worker setting is missing or empty: $required_key" >&2
      exit 1
    fi
  done
  if ! command -v clamdscan >/dev/null || [[ ! -S /run/clamav/clamd.ctl ]] || ! id -nG | tr ' ' '\n' | grep -qx clamav; then
    echo "Enable the host ClamAV Unix socket and grant the deploy user clamav group access before starting media scanning." >&2
    exit 1
  fi
  if ! systemctl --user show-environment >/dev/null 2>&1 || [[ "$(loginctl show-user "$USER" --property=Linger --value 2>/dev/null)" != yes ]]; then
    echo "A lingering systemd user manager is required for the persistent media worker." >&2
    exit 1
  fi

  worker_tmp=$(mktemp -d)
  worker_container=$(docker create "$image")
  if ! docker cp "$worker_container:/app/media-worker" "$worker_tmp/media-worker" || \
    ! docker cp "$worker_container:/app/city4christ-media-worker.service" "$worker_tmp/city4christ-media-worker.service"; then
    docker rm "$worker_container" >/dev/null 2>&1 || true
    rm -rf -- "$worker_tmp"
    echo "The release image does not contain the media worker and its service unit." >&2
    exit 1
  fi
  docker rm "$worker_container" >/dev/null
  worker_bin_dir="$HOME/.local/lib/city4christ/media-worker"
  worker_link="$HOME/.local/bin/city4christ-media-worker"
  unit_dir="$HOME/.config/systemd/user"
  worker_release="$worker_bin_dir/$release_sha"
  old_worker_target=$(readlink "$worker_link" 2>/dev/null || true)
  install -d -m 700 "$worker_bin_dir" "$HOME/.local/bin" "$unit_dir"
  install -m 700 "$worker_tmp/media-worker" "$worker_release"
  install -m 600 "$worker_tmp/city4christ-media-worker.service" "$unit_dir/city4christ-media-worker.service"
  rm -rf -- "$worker_tmp"
  ln -sfn "$worker_release" "$worker_link"
  systemctl --user daemon-reload
  worker_start_ok=false
  if systemctl --user enable city4christ-media-worker.service; then
    if [[ -n "$old_worker_target" ]]; then
      if systemctl --user restart city4christ-media-worker.service; then worker_start_ok=true; fi
    elif systemctl --user start city4christ-media-worker.service; then
      worker_start_ok=true
    fi
  fi
  if [[ "$worker_start_ok" != true ]]; then
    if [[ -n "$old_worker_target" ]]; then
      ln -sfn "$old_worker_target" "$worker_link"
      systemctl --user restart city4christ-media-worker.service >/dev/null 2>&1 || true
    else
      systemctl --user disable --now city4christ-media-worker.service >/dev/null 2>&1 || true
      rm -f "$worker_link"
    fi
    echo "The media worker failed to start; its prior version was restored when available." >&2
    exit 1
  fi
  sleep 2
  if ! systemctl --user is-active --quiet city4christ-media-worker.service; then
    if [[ -n "$old_worker_target" ]]; then
      ln -sfn "$old_worker_target" "$worker_link"
      systemctl --user restart city4christ-media-worker.service >/dev/null 2>&1 || true
    else
      systemctl --user disable --now city4christ-media-worker.service >/dev/null 2>&1 || true
      rm -f "$worker_link"
    fi
    echo "The media worker exited during startup; its prior version was restored when available." >&2
    exit 1
  fi
  echo "Media scanning worker deployment $release_sha is active."
elif [[ -r "$media_env_file" ]] && grep -Eq '^MEDIA_SCAN_ENABLED=false$' "$media_env_file" && systemctl --user show-environment >/dev/null 2>&1; then
  systemctl --user disable --now city4christ-media-worker.service >/dev/null 2>&1 || true
fi
