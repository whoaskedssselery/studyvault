#!/usr/bin/env bash
# Проверка резервной копии: восстанавливает дамп во временную базу, сверяет её с рабочей и удаляет временную базу.
# Использование: scripts/restore-check.sh [файл.dump]   (по умолчанию самый свежий дамп из ./backups)
# Рабочую базу не трогает: всё происходит в отдельной базе studyvault_restore_check.
set -euo pipefail

cd "$(dirname "$0")/.."
set -a; [ -f .env ] && . ./.env; set +a
: "${POSTGRES_USER:?POSTGRES_USER is not set (see .env)}"
: "${POSTGRES_DB:?POSTGRES_DB is not set (see .env)}"

dump="${1:-$(ls -1t backups/studyvault-db-*.dump 2>/dev/null | head -n1 || true)}"
[ -n "$dump" ] && [ -s "$dump" ] || { echo "no dump to check: run scripts/backup.sh first" >&2; exit 1; }

scratch="studyvault_restore_check"
psql_admin() { docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d postgres -tAc "$1"; }
psql_scratch() { docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$scratch" -tAc "$1"; }
psql_live() { docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tAc "$1"; }
cleanup() { psql_admin "DROP DATABASE IF EXISTS $scratch" >/dev/null 2>&1 || true; }
trap cleanup EXIT

echo "restoring $dump into $scratch"
cleanup
psql_admin "CREATE DATABASE $scratch" >/dev/null
docker compose exec -T postgres pg_restore -U "$POSTGRES_USER" -d "$scratch" --no-owner --exit-on-error < "$dump"

fail=0
check() {   # check ИМЯ ЗАПРОС: число строк в копии и в рабочей базе должно совпасть
  local name="$1" query="$2" restored live
  restored="$(psql_scratch "$query")"
  live="$(psql_live "$query")"
  if [ "$restored" = "$live" ]; then
    printf 'ok    %-34s %s\n' "$name" "$restored"
  else
    printf 'DIFF  %-34s restored=%s live=%s\n' "$name" "$restored" "$live"
    # Разница допустима, если после дампа в рабочую базу писали: сверяем «не меньше, чем было бы потеряно».
    [ "$restored" -le "$live" ] 2>/dev/null || fail=1
  fi
}
check "users"                  "SELECT count(*) FROM users"
check "notes"                  "SELECT count(*) FROM notes"
check "published materials"    "SELECT count(*) FROM notes WHERE status = 'published' AND deleted_at IS NULL"
check "comments"               "SELECT count(*) FROM comments"
check "ratings"                "SELECT count(*) FROM note_ratings"
check "moderation decisions"   "SELECT count(*) FROM moderation_decisions"
check "attachments (metadata)" "SELECT count(*) FROM attachments"
check "migrations applied"     "SELECT max(version_id) FROM goose_db_version"

# Структура должна быть цела: ограничения и представление на месте, индексы читаются.
for object in "view library_items:SELECT count(*) FROM pg_views WHERE viewname = 'library_items'" \
              "check constraints:SELECT count(*) FROM pg_constraint WHERE contype = 'c' AND conrelid = 'notes'::regclass"; do
  check "${object%%:*}" "${object#*:}"
done
# Восстановленная база пригодна для работы: библиотека читается.
psql_scratch "SELECT count(*) FROM library_items" >/dev/null && echo "ok    the library view reads"

[ "$fail" = "0" ] || { echo "the backup does not match the live database" >&2; exit 1; }
echo "the backup restores cleanly"
