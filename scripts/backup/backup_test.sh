#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BACKUP_SCRIPT="${SCRIPT_DIR}/backup.sh"

fail() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

pass() {
  printf 'PASS: %s\n' "$*"
}

for cmd in age age-keygen rclone gzip cmp sha256sum grep; do
  command -v "$cmd" >/dev/null 2>&1 || fail "Required tool '${cmd}' is not installed."
done

[[ -x "$BACKUP_SCRIPT" ]] || fail "Backup script is missing or not executable: ${BACKUP_SCRIPT}"

test_root="$(mktemp -d)"
cleanup() {
  chmod -R u+w -- "$test_root" 2>/dev/null || true
  rm -rf -- "$test_root"
}
trap cleanup EXIT INT TERM

# Generate ephemeral primary and wrong age identities; keep private keys out of backup.sh env.
identity_file="${test_root}/identity.txt"
wrong_identity_file="${test_root}/wrong_identity.txt"
age-keygen -o "$identity_file" 2>/dev/null
age-keygen -o "$wrong_identity_file" 2>/dev/null

public_key="$(age-keygen -y "$identity_file")"
secret_key="$(grep -E '^AGE-SECRET-KEY-' "$identity_file")"

[[ "$public_key" =~ ^age1[0-9a-z]{58}$ ]] || fail "Generated invalid public key: ${public_key}"

# Simulate a read-only Kubernetes Secret mount for rclone.conf.
rclone_dir="${test_root}/rclone-ro"
rclone_conf="${rclone_dir}/rclone.conf"
mkdir -p "$rclone_dir"
cat > "$rclone_conf" <<'EOF'
[testremote]
type = local
EOF
chmod 0444 "$rclone_conf"
chmod 0555 "$rclone_dir"

# ---------------------------------------------------------------------------
# 1. Synthetic SQL end-to-end round-trip test
# ---------------------------------------------------------------------------
expected_sql="${test_root}/expected.sql"
cat > "$expected_sql" <<'SQL'
--
-- PostgreSQL database dump
--
SET statement_timeout = 0;
SET client_encoding = 'UTF8';

CREATE TABLE public.accounts (
    id uuid PRIMARY KEY,
    name text NOT NULL
);

ALTER TABLE public.accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE public.accounts FORCE ROW LEVEL SECURITY;

CREATE POLICY accounts_workspace_isolation ON public.accounts
    USING (id IS NOT NULL);

INSERT INTO public.accounts (id, name) VALUES
    ('11111111-1111-1111-1111-111111111111', 'SENTINEL_CONFIDENTIAL_LEDGER_ROW_42');
SQL

fake_pg_dump="${test_root}/fake_pg_dump.sh"
cat > "$fake_pg_dump" <<EOF
#!/usr/bin/env bash
set -euo pipefail

expected_args=(
  "--dbname=postgres://test:secret@localhost:5432/budgetdb"
  "--format=plain"
  "--no-owner"
  "--no-privileges"
  "--clean"
  "--if-exists"
  "--lock-wait-timeout=60000"
)

if [[ "\$#" -ne "\${#expected_args[@]}" ]]; then
  echo "fake_pg_dump: unexpected argument count: \$#" >&2
  exit 1
fi

for i in "\${!expected_args[@]}"; do
  if [[ "\${@:\$((i + 1)):1}" != "\${expected_args[\$i]}" ]]; then
    echo "fake_pg_dump: arg \$i mismatch: got '\${@:\$((i + 1)):1}', want '\${expected_args[\$i]}'" >&2
    exit 1
  fi
done

cat "${expected_sql}"
EOF
chmod +x "$fake_pg_dump"

synth_tmp="${test_root}/synth-tmp"
synth_remote="${test_root}/synth-remote"
mkdir -p "$synth_tmp" "$synth_remote"

DATABASE_URL="postgres://test:secret@localhost:5432/budgetdb" \
AGE_PUBLIC_KEY="$public_key" \
RCLONE_CONFIG="$rclone_conf" \
RCLONE_REMOTE="testremote:${synth_remote}" \
BACKUP_TIMESTAMP="20261125T020000Z" \
BACKUP_TMPDIR="$synth_tmp" \
PG_DUMP_BIN="$fake_pg_dump" \
  "$BACKUP_SCRIPT"

synth_archive="${synth_remote}/budget_backup_20261125T020000Z.sql.gz.age"
[[ -s "$synth_archive" ]] || fail "Synthetic encrypted archive missing or empty: ${synth_archive}"

[[ "$(head -n 1 "$synth_archive")" == "age-encryption.org/v1" ]] || fail "Archive missing age header"

if grep -aq "SENTINEL_CONFIDENTIAL_LEDGER_ROW_42" "$synth_archive" || grep -aq "CREATE TABLE" "$synth_archive"; then
  fail "Plaintext SQL leaked into encrypted archive"
fi

if age --decrypt -i "$wrong_identity_file" "$synth_archive" >/dev/null 2>&1; then
  fail "Archive unexpectedly decrypted with wrong private key"
fi

restored_sql="${test_root}/restored.sql"
age --decrypt -i "$identity_file" "$synth_archive" | gzip -d > "$restored_sql"
cmp -s "$expected_sql" "$restored_sql" || fail "Restored SQL does not match input byte-for-byte"

if find "$synth_tmp" -maxdepth 1 -name 'budget-backup.*' | grep -q .; then
  fail "Temporary working directory leaked in BACKUP_TMPDIR after synthetic backup"
fi

pass "Synthetic SQL dump -> gzip -> asymmetric age encryption -> rclone copy -> decrypt round-trip"

# ---------------------------------------------------------------------------
# 2. Live PostgreSQL pg_dump round-trip test
# ---------------------------------------------------------------------------
live_db_url="${BACKUP_TEST_DATABASE_URL:-${DATABASE_URL:-postgres://postgres:postgres@127.0.0.1:5432/budgetdb?sslmode=disable}}"
if command -v pg_dump >/dev/null 2>&1 && pg_dump --dbname="$live_db_url" --schema-only >/dev/null 2>&1; then
  live_tmp="${test_root}/live-tmp"
  live_remote="${test_root}/live-remote"
  mkdir -p "$live_tmp" "$live_remote"

  DATABASE_URL="$live_db_url" \
  AGE_PUBLIC_KEY="$public_key" \
  RCLONE_CONFIG="$rclone_conf" \
  RCLONE_REMOTE="testremote:${live_remote}" \
  BACKUP_TIMESTAMP="20261125T020001Z" \
  BACKUP_TMPDIR="$live_tmp" \
    "$BACKUP_SCRIPT"

  live_archive="${live_remote}/budget_backup_20261125T020001Z.sql.gz.age"
  [[ -s "$live_archive" ]] || fail "Live PostgreSQL encrypted archive missing or empty"

  live_restored="${test_root}/live_restored.sql"
  age --decrypt -i "$identity_file" "$live_archive" | gzip -d > "$live_restored"

  grep -q -- "-- PostgreSQL database dump" "$live_restored" || fail "Live dump missing PostgreSQL header"
  grep -q "SET statement_timeout" "$live_restored" || fail "Live dump missing SET statement_timeout"

  if find "$live_tmp" -maxdepth 1 -name 'budget-backup.*' | grep -q .; then
    fail "Temporary working directory leaked in BACKUP_TMPDIR after live backup"
  fi

  pass "Live PostgreSQL pg_dump -> gzip -> age -> rclone -> decrypt round-trip"
else
  if [[ "${BACKUP_TEST_REQUIRE_POSTGRES:-0}" == "1" ]]; then
    fail "PostgreSQL is required (BACKUP_TEST_REQUIRE_POSTGRES=1) but unreachable at configured URL"
  fi
  printf 'SKIP: Live PostgreSQL pg_dump round-trip (database unreachable)\n'
fi

# ---------------------------------------------------------------------------
# 3. Security and failure-mode guard tests
# ---------------------------------------------------------------------------
expect_fail() {
  local desc="$1"
  shift
  local output=""
  if output="$("$@" 2>&1)"; then
    fail "Expected failure for: ${desc}"
  fi
  printf '%s' "$output"
}

expect_fail "missing DATABASE_URL" env -u DATABASE_URL \
  AGE_PUBLIC_KEY="$public_key" \
  RCLONE_CONFIG="$rclone_conf" \
  "$BACKUP_SCRIPT" >/dev/null
pass "Guard: missing DATABASE_URL fails"

expect_fail "missing AGE_PUBLIC_KEY" env -u AGE_PUBLIC_KEY \
  DATABASE_URL="postgres://test:secret@localhost:5432/budgetdb" \
  RCLONE_CONFIG="$rclone_conf" \
  "$BACKUP_SCRIPT" >/dev/null
pass "Guard: missing AGE_PUBLIC_KEY fails"

secret_out="$(expect_fail "AGE-SECRET-KEY passed as AGE_PUBLIC_KEY" env \
  DATABASE_URL="postgres://test:secret@localhost:5432/budgetdb" \
  AGE_PUBLIC_KEY="$secret_key" \
  RCLONE_CONFIG="$rclone_conf" \
  "$BACKUP_SCRIPT")"
echo "$secret_out" | grep -qi "refusing.*private key" || fail "Private key rejection message missing: ${secret_out}"
pass "Guard: refuses AGE-SECRET-KEY-* in AGE_PUBLIC_KEY"

expect_fail "malformed AGE_PUBLIC_KEY" env \
  DATABASE_URL="postgres://test:secret@localhost:5432/budgetdb" \
  AGE_PUBLIC_KEY="not-a-valid-key" \
  RCLONE_CONFIG="$rclone_conf" \
  "$BACKUP_SCRIPT" >/dev/null
pass "Guard: malformed AGE_PUBLIC_KEY fails"

expect_fail "missing RCLONE_CONFIG" env \
  DATABASE_URL="postgres://test:secret@localhost:5432/budgetdb" \
  AGE_PUBLIC_KEY="$public_key" \
  RCLONE_CONFIG="${test_root}/does-not-exist.conf" \
  "$BACKUP_SCRIPT" >/dev/null
pass "Guard: missing RCLONE_CONFIG fails"

failing_pg_dump="${test_root}/failing_pg_dump.sh"
cat > "$failing_pg_dump" <<'EOF'
#!/usr/bin/env bash
echo "-- partial SQL output before failure"
exit 2
EOF
chmod +x "$failing_pg_dump"

fail_tmp="${test_root}/fail-tmp"
fail_remote="${test_root}/fail-remote"
mkdir -p "$fail_tmp" "$fail_remote"

expect_fail "failing pg_dump aborts via pipefail without uploading" env \
  DATABASE_URL="postgres://test:secret@localhost:5432/budgetdb" \
  AGE_PUBLIC_KEY="$public_key" \
  RCLONE_CONFIG="$rclone_conf" \
  RCLONE_REMOTE="testremote:${fail_remote}" \
  BACKUP_TIMESTAMP="20261125T020002Z" \
  BACKUP_TMPDIR="$fail_tmp" \
  PG_DUMP_BIN="$failing_pg_dump" \
  "$BACKUP_SCRIPT" >/dev/null

if find "$fail_remote" -mindepth 1 | grep -q .; then
  fail "Partial archive was uploaded despite pg_dump failure"
fi

if find "$fail_tmp" -mindepth 1 | grep -q .; then
  fail "Temporary files leaked in BACKUP_TMPDIR after pg_dump failure"
fi
pass "Guard: failing pg_dump aborts via pipefail, uploads nothing, and cleans up temp dir"

printf 'ALL BACKUP TESTS PASSED\n'
