#!/usr/bin/env bash
# Резервная копия базы и файлов. Использование: scripts/backup.sh [каталог]   (по умолчанию ./backups)
#
# Делает две вещи:
#   1. дамп Postgres в формате custom (pg_dump -Fc): внутри сжат и восстанавливается выборочно;
#   2. архив тома с файлами S3 (вложения материалов).
# Старше KEEP_DAYS суток (по умолчанию 14) копии удаляются. Запускается раз в сутки планировщиком, пример в
# docs/operations.md. Проверка, что копию действительно можно восстановить, делает scripts/restore-check.sh.
set -euo pipefail

dir="${1:-backups}"
keep_days="${KEEP_DAYS:-14}"
stamp="$(date +%Y%m%d-%H%M%S)"
mkdir -p "$dir"
# Абсолютный путь нужен для монтирования в контейнер; pwd -W даёт его в виде, понятном Docker в Git Bash на Windows.
abs_dir="$(cd "$dir" && (pwd -W 2>/dev/null || pwd))"

# Параметры подключения берутся из .env рядом с docker-compose.yml, как и у самого стека.
cd "$(dirname "$0")/.."
set -a; [ -f .env ] && . ./.env; set +a
: "${POSTGRES_USER:?POSTGRES_USER is not set (see .env)}"
: "${POSTGRES_DB:?POSTGRES_DB is not set (see .env)}"

db_file="$dir/studyvault-db-$stamp.dump"
docker compose exec -T postgres pg_dump -U "$POSTGRES_USER" -d "$POSTGRES_DB" -Fc --no-owner > "$db_file.partial"
mv "$db_file.partial" "$db_file"   # неполный файл никогда не выглядит готовой копией
[ -s "$db_file" ] || { echo "the database dump is empty" >&2; exit 1; }
echo "database: $db_file ($(wc -c < "$db_file" | tr -d ' ') bytes)"

# Том с файлами. Имя тома зависит от имени проекта compose, поэтому берём его у сервиса.
s3_container="$(docker compose ps -q s3 || true)"
if [ -n "$s3_container" ]; then
  files_file="$dir/studyvault-files-$stamp.tar.gz"
  MSYS_NO_PATHCONV=1 docker run --rm --volumes-from "$s3_container" -v "$abs_dir:/backup" alpine \
    tar -czf "/backup/$(basename "$files_file")" -C /data .
  echo "files:    $files_file ($(wc -c < "$files_file" | tr -d ' ') bytes)"
else
  echo "the s3 service is not running: files were not backed up" >&2
fi

find "$dir" -maxdepth 1 -name 'studyvault-*' -type f -mtime "+$keep_days" -print -delete | sed 's/^/removed old copy: /'
