#!/usr/bin/env bash
# Нагрузочная проверка по ТЗ (N3): 1000 материалов и 20 одновременных пользователей, список отвечает
# до 2 секунд для 95% запросов. Временные данные создаются прямо в базе, помечаются и в конце удаляются.
# Нужен поднятый стек. Использование: scripts/loadtest.sh [адрес API] [число материалов] [запросов на пользователя]
set -euo pipefail

cd "$(dirname "$0")/.."
set -a; [ -f .env ] && . ./.env; set +a
base="${1:-http://localhost:8080}"
materials="${2:-1000}"
per_user="${3:-25}"
users=20
limit_ms=2000
marker="loadtest-$RANDOM$RANDOM"
tmp="$(mktemp -d)"

psql() { docker compose exec -T postgres psql -v ON_ERROR_STOP=1 -q -U "$POSTGRES_USER" -d "$POSTGRES_DB" -tA "$@"; }
cleanup() {
  psql -c "DELETE FROM users WHERE email LIKE '$marker-%'" >/dev/null 2>&1 || true
  psql -c "DELETE FROM library_subjects WHERE title LIKE '$marker subject %'" >/dev/null 2>&1 || true
  rm -rf "$tmp"
}
trap cleanup EXIT

echo "creating $materials published materials ($marker)"
psql <<SQL
INSERT INTO users (email, password_hash, display_name, faculty)
SELECT '$marker-' || g || '@load.test', 'x', 'Load author ' || g, 'Load faculty' FROM generate_series(1, 50) g;
INSERT INTO library_subjects (title) SELECT '$marker subject ' || g FROM generate_series(1, 5) g;
INSERT INTO notes (user_id, title, content, content_text, kind, summary, semester, status, published_at, views, library_subject_id)
SELECT (SELECT id FROM users WHERE email = '$marker-' || (1 + g % 50) || '@load.test'),
       'Material ' || g || ' about derivatives and limits',
       '{"type":"doc","content":[]}'::jsonb,
       'text of material ' || g || ' with several words about calculus',
       CASE WHEN g % 3 = 0 THEN 'cheat_sheet' ELSE 'note' END,
       'summary ' || g, 1 + g % 8, 'published', now() - make_interval(mins => g), (g * 7919) % 1000,
       (SELECT id FROM library_subjects WHERE title = '$marker subject ' || (1 + g % 5))
FROM generate_series(1, $materials) g;
INSERT INTO note_ratings (note_id, user_id, value)
SELECT n.id, u.id, 1 + (abs(hashtext(n.id::text || u.id::text)) % 5)
FROM (SELECT id FROM notes WHERE title LIKE 'Material % about derivatives and limits' AND status = 'published' ORDER BY id LIMIT 600) n
JOIN (SELECT id FROM users WHERE email LIKE '$marker-%' LIMIT 8) u ON n.id IS NOT NULL;
ANALYZE notes; ANALYZE note_ratings;
SQL
total="$(psql -c "SELECT count(*) FROM library_items")"
echo "the library holds $total published materials"

subject="$(psql -c "SELECT id FROM library_subjects WHERE title = '$marker subject 1'")"
urls=(
  "/v1/library"
  "/v1/library?limit=50"
  "/v1/library?sort=rating"
  "/v1/library?sort=views&limit=50"
  "/v1/library?library_subject_id=$subject&sort=rating"
  "/v1/library?kind=cheat_sheet&semester=3"
  "/v1/library-subjects"
)
: > "$tmp/urls"
for _ in $(seq 1 "$per_user"); do printf '%s\n' "${urls[@]}" >> "$tmp/urls"; done
# каждый «пользователь» проходит свою перемешанную очередь одним потоком, пользователи идут одновременно
for u in $(seq 1 $users); do
  ( sort -R "$tmp/urls" | head -n "$per_user" | while read -r path; do
      curl -sS -o /dev/null -w '%{http_code} %{time_total}\n' "$base$path"
    done > "$tmp/user-$u.log" ) &
done
started="$(date +%s)"
wait
elapsed=$(( $(date +%s) - started ))

cat "$tmp"/user-*.log > "$tmp/all.log"
n="$(wc -l < "$tmp/all.log" | tr -d ' ')"
bad="$(awk '$1 != 200' "$tmp/all.log" | wc -l | tr -d ' ')"
sort -k2 -n "$tmp/all.log" | awk -v n="$n" '{ t[NR] = $2 * 1000 } END {
  p50 = t[int(n * 0.50 + 0.5)]; p95 = t[int(n * 0.95 + 0.5)]; p99 = t[int(n * 0.99 + 0.5)]; max = t[n]
  printf "requests: %d, p50 %.0f ms, p95 %.0f ms, p99 %.0f ms, max %.0f ms\n", n, p50, p95, p99, max
  print p95 > "'"$tmp"'/p95" }'
echo "concurrent users: $users, wall time: ${elapsed}s, non-200 responses: $bad"
p95="$(cat "$tmp/p95")"
[ "$bad" = 0 ] || { echo "FAIL: some responses were not 200" >&2; exit 1; }
awk -v p="$p95" -v l="$limit_ms" 'BEGIN { exit (p + 0 <= l) ? 0 : 1 }' \
  && echo "OK: p95 is within the ${limit_ms} ms target of the requirements" \
  || { echo "FAIL: p95 ${p95} ms is above the ${limit_ms} ms target" >&2; exit 1; }
