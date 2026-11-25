#!/usr/bin/env bash
set -euo pipefail
umask 077

log() {
  printf '%s [backup] %s\n' "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" "$*"
}

err() {
  printf '%s [backup] ERROR: %s\n' "$(date -u +"%Y-%m-%dT%H:%M:%SZ")" "$*" >&2
}

require_env() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    err "Required environment variable ${name} is unset or empty."
    exit 1
  fi
}

require_bin() {
  local bin="$1"
  if ! command -v "$bin" >/dev/null 2>&1; then
    err "Required executable '${bin}' not found in PATH."
    exit 1
  fi
}

require_env "DATABASE_URL"
require_env "AGE_PUBLIC_KEY"

if [[ "$AGE_PUBLIC_KEY" == AGE-SECRET-KEY-* ]]; then
  err "Refusing to run with an age private key; only the public recipient key may be stored on the cluster."
  exit 1
fi

if [[ ! "$AGE_PUBLIC_KEY" =~ ^age1[0-9a-z]{58}$ ]]; then
  err "Invalid AGE_PUBLIC_KEY format; expected an asymmetric age1 public recipient key."
  exit 1
fi

RCLONE_CONFIG="${RCLONE_CONFIG:-/etc/rclone/rclone.conf}"
RCLONE_REMOTE="${RCLONE_REMOTE:-gdrive:HomelabBackups/BudgetApp/}"
BACKUP_PREFIX="${BACKUP_PREFIX:-budget_backup}"
BACKUP_TIMESTAMP="${BACKUP_TIMESTAMP:-$(date -u +"%Y%m%dT%H%M%SZ")}"
BACKUP_FILENAME="${BACKUP_FILENAME:-${BACKUP_PREFIX}_${BACKUP_TIMESTAMP}.sql.gz.age}"
BACKUP_TMPDIR="${BACKUP_TMPDIR:-/tmp}"
RCLONE_RETRIES="${RCLONE_RETRIES:-3}"

if [[ -z "$RCLONE_REMOTE" ]]; then
  err "RCLONE_REMOTE must not be empty."
  exit 1
fi

if [[ ! -f "$RCLONE_CONFIG" || ! -r "$RCLONE_CONFIG" ]]; then
  err "rclone configuration file not found or unreadable at: ${RCLONE_CONFIG}"
  exit 1
fi

PG_DUMP_BIN="${PG_DUMP_BIN:-pg_dump}"
GZIP_BIN="${GZIP_BIN:-gzip}"
AGE_BIN="${AGE_BIN:-age}"
RCLONE_BIN="${RCLONE_BIN:-rclone}"

require_bin "$PG_DUMP_BIN"
require_bin "$GZIP_BIN"
require_bin "$AGE_BIN"
require_bin "$RCLONE_BIN"

work_dir=""
cleanup() {
  if [[ -n "${work_dir:-}" && -d "$work_dir" ]]; then
    rm -rf -- "$work_dir"
  fi
}
trap cleanup EXIT INT TERM

work_dir="$(mktemp -d "${BACKUP_TMPDIR%/}/budget-backup.XXXXXX")"

# Copy rclone.conf into ephemeral writable storage so OAuth2 token refreshes
# succeed even when /etc/rclone is mounted read-only from a Kubernetes Secret.
effective_rclone_config="${work_dir}/rclone.conf"
cp "$RCLONE_CONFIG" "$effective_rclone_config"
chmod 600 "$effective_rclone_config"

archive_path="${work_dir}/${BACKUP_FILENAME}"
start_epoch="$(date -u +%s)"

"$PG_DUMP_BIN" \
  --dbname="$DATABASE_URL" \
  --format=plain \
  --no-owner \
  --no-privileges \
  --clean \
  --if-exists \
  --lock-wait-timeout=60000 \
  | "$GZIP_BIN" -9 \
  | "$AGE_BIN" -r "$AGE_PUBLIC_KEY" \
  > "$archive_path"

if [[ ! -s "$archive_path" ]]; then
  err "Encrypted backup archive is empty: ${BACKUP_FILENAME}"
  exit 1
fi

first_line="$(head -n 1 "$archive_path")"
if [[ "$first_line" != "age-encryption.org/v1" ]]; then
  err "Encrypted archive header check failed for ${BACKUP_FILENAME}"
  exit 1
fi

archive_bytes="$(wc -c < "$archive_path" | tr -d '[:space:]')"
archive_sha256="$(sha256sum "$archive_path" | awk '{print $1}')"

"$RCLONE_BIN" \
  --config "$effective_rclone_config" \
  copy \
  --no-traverse \
  --retries "$RCLONE_RETRIES" \
  --low-level-retries 5 \
  --contimeout 30s \
  --timeout 5m \
  "$archive_path" \
  "$RCLONE_REMOTE"

end_epoch="$(date -u +%s)"
duration_s=$((end_epoch - start_epoch))

log "Backup complete: archive=${BACKUP_FILENAME} bytes=${archive_bytes} sha256=${archive_sha256} remote=${RCLONE_REMOTE} duration_s=${duration_s}"
