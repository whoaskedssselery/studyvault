#!/usr/bin/env bash
# Сквозная проверка запущенного стека (docker compose --profile app up): пользователь проходит основной путь
# через настоящий HTTP, настоящие Postgres, Meilisearch и S3. Использование: scripts/smoke.sh [адрес API]
set -euo pipefail

base="${1:-http://localhost:8080}"
email="smoke-$(date +%s)-$RANDOM@example.com"
password="smoke-password-1"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

step() { printf '\n== %s\n' "$*"; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }

# call МЕТОД ПУТЬ [ТЕЛО]: путь дан относительно /v1. Тело ответа в $tmp/body, код в $code
call() {
  local method="$1" path="$2" body="${3:-}"
  local url="$base/v1$path"
  case "$path" in /docs|/openapi.yaml) url="$base$path" ;; esac   # документация живёт вне версии API
  local args=(-sS -o "$tmp/body" -D "$tmp/headers" -w '%{http_code}' -X "$method" "$url")
  [ -n "${token:-}" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-H 'Content-Type: application/json' --data "$body")
  code="$(curl "${args[@]}")"
}
expect() { [ "$code" = "$1" ] || fail "$2: HTTP $code, expected $1: $(cat "$tmp/body")"; echo "ok  $2 ($code)"; }
# jget a.b.c: значение из JSON последнего ответа (perl есть и в Git Bash, и на ubuntu-runner, а jq не везде).
jget() { perl -MJSON::PP -e 'local $/; my $d = decode_json(<STDIN>); $d = $d->{$_} for split /\./, shift; print $d // ""' "$1" < "$tmp/body"; }

token=""

step "service is alive"
call GET /health;                 expect 200 "GET /health"
grep -q '"ok"' "$tmp/body" || fail "health body: $(cat "$tmp/body")"
grep -qi '^x-request-id: ' "$tmp/headers" || fail "responses must carry X-Request-ID"

step "versioning"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$base/health")" = "200" ] || fail "the unversioned liveness probe /health must answer 200"
[ "$(curl -sS -o /dev/null -w '%{http_code}' "$base/subjects")" = "404" ] || fail "paths without /v1 must not exist"
echo "ok  /health answers, unprefixed API paths are gone"

step "documentation is served"
call GET /docs;                   expect 200 "GET /docs"
call GET /openapi.yaml;           expect 200 "GET /openapi.yaml"

step "closed without a token"
call GET /subjects;               expect 401 "GET /subjects without a token"

step "registration and account"
call POST /auth/register "{\"email\":\"$email\",\"password\":\"$password\",\"display_name\":\"Smoke Test\"}"
expect 201 "POST /auth/register"
token="$(jget access_token)"; [ -n "$token" ] || fail "no access_token in the response"
call GET /me;                     expect 200 "GET /me"
grep -q "$email" "$tmp/body" || fail "GET /me does not return the account: $(cat "$tmp/body")"

step "subject, notes and a link between them"
call POST /subjects '{"title":"Smoke subject"}';  expect 201 "POST /subjects"
subject="$(jget id)"
call POST /notes "{\"title\":\"Derivatives\",\"subject_id\":\"$subject\",\"content\":{\"type\":\"doc\",\"content\":[{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"rates of change\"}]}]}}"
expect 201 "POST /notes (target)"
target="$(jget id)"
call POST /notes '{"title":"Limits","content":{"type":"doc","content":[{"type":"paragraph","content":[{"type":"text","text":"see [[Derivatives]] for more"}]}]}}'
expect 201 "POST /notes (source)"
call GET "/notes/$target/backlinks";   expect 200 "GET backlinks"
grep -q 'Limits' "$tmp/body" || fail "backlink is missing: $(cat "$tmp/body")"
call GET /graph;                  expect 200 "GET /graph"

step "full-text search"
found=""
for _ in $(seq 1 20); do   # индекс обновляется асинхронно
  call GET '/search?q=derivatives'
  if [ "$code" = "200" ] && grep -q 'Derivatives' "$tmp/body"; then found=1; break; fi
  sleep 0.5
done
[ -n "$found" ] || fail "search did not find the note: HTTP $code $(cat "$tmp/body")"
echo "ok  GET /search finds the note"

step "file upload and download through S3"
printf 'hello from the smoke test' > "$tmp/file.txt"
size="$(wc -c < "$tmp/file.txt" | tr -d ' ')"
call POST /attachments "{\"note_id\":\"$target\",\"file_name\":\"smoke.txt\",\"mime_type\":\"text/plain\",\"size_bytes\":$size}"
expect 201 "POST /attachments"
attachment="$(jget attachment.id)"
upload_url="$(jget upload_url)"
[ -n "$upload_url" ] || fail "no upload_url: $(cat "$tmp/body")"
put="$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: text/plain' --data-binary "@$tmp/file.txt" "$upload_url")"
[ "$put" = "200" ] || fail "PUT to the presigned URL: HTTP $put"
echo "ok  PUT to the presigned URL (200)"
call GET "/attachments/$attachment";   expect 200 "GET /attachments/{id}"
download_url="$(jget download_url)"
[ "$(curl -sS "$download_url")" = "hello from the smoke test" ] || fail "downloaded content differs"
echo "ok  downloaded content matches"

step "password change ends the old password"
call POST /me/password "{\"current_password\":\"$password\",\"new_password\":\"smoke-password-2\"}"
expect 200 "POST /me/password"
token="$(jget access_token)"
call POST /auth/login "{\"email\":\"$email\",\"password\":\"$password\"}";            expect 401 "login with the old password"
call POST /auth/login "{\"email\":\"$email\",\"password\":\"smoke-password-2\"}";    expect 200 "login with the new password"

printf '\nAll smoke checks passed.\n'
