#!/usr/bin/env bash
# Проверка из ТЗ (N3): после штатного перезапуска сервер восстанавливает доступ к сохранённым данным.
# Создаёт и публикует материал, перезапускает все контейнеры, затем проверяет GET /health, повторное открытие
# материала и сохранность файла. Нужен поднятый стек с демо-данными (seed). Использование: scripts/restart-check.sh [адрес API]
set -euo pipefail

cd "$(dirname "$0")/.."
base="${1:-http://localhost:8080}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
req() { # req МЕТОД ПУТЬ [ТОКЕН] [ТЕЛО] -> тело в $tmp/body, код в $code
  local args=(-sS -o "$tmp/body" -w '%{http_code}' -X "$1" "$base/v1$2")
  [ -n "${3:-}" ] && args+=(-H "Authorization: Bearer $3")
  [ -n "${4:-}" ] && args+=(-H 'Content-Type: application/json' --data "$4")
  code="$(curl "${args[@]}")"
}
jget() { perl -MJSON::PP -e 'local $/; my $d = decode_json(<STDIN>); $d = ref $d eq "ARRAY" ? $d->[$_] : $d->{$_} for split /\./, shift; print $d // ""' "$1" < "$tmp/body"; }

pw="${SEED_PASSWORD:-demo-password-123}"
# Три запроса к /auth; если рядом только что шёл другой прогон, лимит попыток входа (20 в минуту) мог быть исчерпан.
author="restart-$(date +%s)-$RANDOM@example.com"
req POST /auth/register "" "{\"email\":\"$author\",\"password\":\"$pw-author\",\"display_name\":\"Restart Check\"}"; [ "$code" = 201 ] || fail "register: $code"
at="$(jget access_token)"
# Публиковать можно только с подтверждённой почтой: ссылку берём из письма в Mailpit (при настроенной почте).
for _ in $(seq 1 20); do
  curl -sS -G -o "$tmp/mail" --data-urlencode "query=to:$author" "${MAILPIT_URL:-http://localhost:8025}/api/v1/search" || true
  mid="$(perl -MJSON::PP -e 'local $/; my $d = eval { decode_json(<STDIN>) } or exit; print $d->{messages}[0]{ID} // ""' < "$tmp/mail")"
  [ -n "$mid" ] && break; sleep 0.5
done
[ -n "$mid" ] || fail "no verification mail in Mailpit"
curl -sS -o "$tmp/mail" "${MAILPIT_URL:-http://localhost:8025}/api/v1/message/$mid"
vt="$(perl -MJSON::PP -e 'local $/; my $d = decode_json(<STDIN>); print $1 if $d->{Text} =~ m{verify-email\?token=([A-Za-z0-9_-]+)}' < "$tmp/mail")"
req POST /auth/email-verification/confirm "" "{\"token\":\"$vt\"}"; [ "$code" = 204 ] || fail "verify e-mail: $code"
req POST /auth/login "" "{\"email\":\"moderator@example.com\",\"password\":\"$pw\"}"; [ "$code" = 200 ] || fail "moderator login: $code (run the seed first)"
mt="$(jget access_token)"
req GET /library-subjects; subject="$(jget items.0.id)"

word="restartword$RANDOM$RANDOM"
req POST /notes "$at" "{\"title\":\"Survives a restart $word\",\"library_subject_id\":\"$subject\",\"content\":{\"type\":\"doc\",\"content\":[{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"$word\"}]}]}}"
[ "$code" = 201 ] || fail "create: $code"; id="$(jget id)"
printf 'file survives the restart' > "$tmp/f.pdf"
req POST /attachments "$at" "{\"note_id\":\"$id\",\"file_name\":\"f.pdf\",\"mime_type\":\"application/pdf\",\"size_bytes\":$(wc -c < "$tmp/f.pdf" | tr -d ' ')}"
[ "$code" = 201 ] || fail "attachment: $code"; att="$(jget attachment.id)"; up="$(jget upload_url)"
[ "$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: application/pdf' --data-binary "@$tmp/f.pdf" "$up")" = 200 ] || fail "upload"
req POST "/notes/$id/submit" "$at"; [ "$code" = 200 ] || fail "submit: $code"
req POST "/moderation/$id/decision" "$mt" '{"decision":"approved"}'; [ "$code" = 200 ] || fail "approve: $code"
echo "published material $id with a file; restarting the whole stack"

# migrate одноразовый, его не перезапускаем: он отработал при старте стека.
docker compose --profile app restart postgres meilisearch s3 api >/dev/null
for _ in $(seq 1 60); do curl -fsS "$base/health" >/dev/null 2>&1 && break; sleep 1; done
curl -fsS "$base/health" | grep -q '"ok"' || fail "GET /health after the restart"
echo "ok  GET /health after the restart"

req GET "/library/$id"; [ "$code" = 200 ] && [ "$(jget status)" = published ] || fail "the material after the restart: $code"
echo "ok  the published material opens again"
req GET "/library/$id/attachments/$att"; [ "$code" = 200 ] || fail "file link after the restart: $code"
dl="$(jget download_url)"
[ "$(curl -sS "$dl")" = "file survives the restart" ] || fail "the stored file changed"
echo "ok  the file is still available and intact"
found=""
for _ in $(seq 1 20); do
  req GET "/library?q=$word"; if grep -q "$id" "$tmp/body"; then found=1; break; fi; sleep 0.5
done
[ -n "$found" ] || fail "search lost the material after the restart"
echo "ok  search still finds it"
echo "The data survived the restart."
