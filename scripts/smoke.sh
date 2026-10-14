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
# jget a.b.0.c: значение из JSON последнего ответа (perl есть и в Git Bash, и на ubuntu-runner, а jq не везде).
jget() { perl -CO -MJSON::PP -e 'local $/; my $d = decode_json(<STDIN>); $d = ref $d eq "ARRAY" ? $d->[$_] : $d->{$_} for split /\./, shift; print $d // ""' "$1" < "$tmp/body"; }

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
grep -qi '^content-type: application/problem+json' "$tmp/headers" || fail "errors must be application/problem+json"
grep -q '"code":"unauthorized"' "$tmp/body" || fail "the error body must carry a code: $(cat "$tmp/body")"

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
call POST /attachments "{\"note_id\":\"$target\",\"file_name\":\"smoke.pdf\",\"mime_type\":\"application/pdf\",\"size_bytes\":$size}"
expect 201 "POST /attachments"
attachment="$(jget attachment.id)"
upload_url="$(jget upload_url)"
[ -n "$upload_url" ] || fail "no upload_url: $(cat "$tmp/body")"
put="$(curl -sS -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: application/pdf' --data-binary "@$tmp/file.txt" "$upload_url")"
[ "$put" = "200" ] || fail "PUT to the presigned URL: HTTP $put"
echo "ok  PUT to the presigned URL (200)"
call GET "/attachments/$attachment";   expect 200 "GET /attachments/{id}"
download_url="$(jget download_url)"
[ "$(curl -sS "$download_url")" = "hello from the smoke test" ] || fail "downloaded content differs"
echo "ok  downloaded content matches"

step "library: from a draft to a published material"
author_token="$token"
call GET /library-subjects;       expect 200 "GET /library-subjects (a guest may read)"
subject_id="$(jget items.0.id)"
[ -n "$subject_id" ] || fail "no shared subjects: run the seed first (docker compose --profile app run --rm --entrypoint /seed api)"
word="smokeword$RANDOM$RANDOM"
call POST /notes "{\"title\":\"Smoke material $word\",\"kind\":\"cheat_sheet\",\"semester\":2,\"summary\":\"made by the smoke test\",\"library_subject_id\":\"$subject_id\",\"content\":{\"type\":\"doc\",\"content\":[{\"type\":\"paragraph\",\"content\":[{\"type\":\"text\",\"text\":\"the unique word is $word\"}]}]}}"
expect 201 "POST /notes (a material draft)"
material="$(jget id)"
token=""
call GET "/library/$material";    expect 404 "a draft is invisible to a guest"
token="$author_token"
call POST "/notes/$material/submit";   expect 200 "POST /notes/{id}/submit"
[ "$(jget status)" = "pending" ] || fail "the material must be pending: $(cat "$tmp/body")"

token=""
call POST /auth/login "{\"email\":\"${MODERATOR_EMAIL:-moderator@example.com}\",\"password\":\"${MODERATOR_PASSWORD:-demo-password-123}\"}"
expect 200 "the moderator signs in (seeded account)"
moderator_token="$(jget access_token)"
token="$author_token"
call POST "/moderation/$material/decision" '{"decision":"approved"}';  expect 403 "the author cannot decide (not a moderator)"
token="$moderator_token"
call GET "/moderation/queue?limit=100";  expect 200 "GET /moderation/queue"
grep -q "$material" "$tmp/body" || fail "the submitted material is not in the queue"
call POST "/moderation/$material/decision" '{"decision":"rejected"}';  expect 400 "a rejection without a reason is refused"
call POST "/moderation/$material/decision" '{"decision":"approved"}';  expect 200 "the moderator approves"
call POST "/moderation/$material/decision" '{"decision":"approved"}';  expect 409 "a second decision is an invalid transition"

token=""
call GET "/library/$material";    expect 200 "a guest reads the published material"
[ "$(jget status)" = "published" ] || fail "status must be published"
call GET "/library?limit=100&library_subject_id=$subject_id"; expect 200 "GET /library (the feed)"
grep -q "$material" "$tmp/body" || fail "the published material is not in the feed"

step "library: a reader comments, rates and saves"
reader="reader-$(date +%s)-$RANDOM@example.com"
call POST /auth/register "{\"email\":\"$reader\",\"password\":\"$password\",\"display_name\":\"Smoke Reader\"}"
expect 201 "a second student registers"
token="$(jget access_token)"
reader_token="$token"
call POST "/library/$material/comments" '{"body":"Thanks, this helped!"}';   expect 201 "POST comment"
comment_id="$(jget id)"
call POST "/library/$material/comments" '{"body":"   "}';                    expect 400 "an empty comment is refused"
call PUT "/library/$material/rating" '{"value":4}';                          expect 200 "PUT rating"
[ "$(jget rating.average)" = "4" ] || fail "the average must be 4: $(cat "$tmp/body")"
call PUT "/library/$material/rating" '{"value":6}';                          expect 400 "a rating of 6 is refused"
call PUT "/library/$material/bookmark";                                      expect 204 "PUT bookmark"
call GET /bookmarks;                                                         expect 200 "GET /bookmarks"
grep -q "$material" "$tmp/body" || fail "the bookmark is missing"
token="$author_token"
call PUT "/library/$material/rating" '{"value":5}';                          expect 403 "the author cannot rate their own material"
token=""
call GET "/library/$material/comments";   expect 200 "a guest reads the comments"
grep -q "Thanks, this helped" "$tmp/body" || fail "the comment is missing"

step "library: search finds it, an edit sends it back to review"
found=""
for _ in $(seq 1 20); do   # индекс обновляется асинхронно
  call GET "/library?q=$word"
  if [ "$code" = "200" ] && grep -q "$material" "$tmp/body"; then found=1; break; fi
  sleep 0.5
done
[ -n "$found" ] || fail "library search did not find the material: HTTP $code $(cat "$tmp/body")"
echo "ok  GET /library?q= finds the published material"
token="$author_token"
call PATCH "/notes/$material" '{"title":"Edited after publication"}';  expect 200 "the author edits the published material"
[ "$(jget status)" = "pending" ] || fail "an edited material must go back to review"
token=""
call GET "/library/$material";    expect 404 "it left the library until it is approved again"
token="$author_token"

step "library: withdraw, tags, authors, comment edit, take-down"
token="$author_token"
call POST "/notes/$material/withdraw";   expect 200 "the author withdraws the pending material"
[ "$(jget status)" = "draft" ] || fail "a withdrawn material must be a draft: $(cat "$tmp/body")"
call POST "/notes/$material/withdraw";   expect 409 "a draft cannot be withdrawn"
call POST "/notes/$material/submit";     expect 200 "the author submits it again"
token="$moderator_token"
call POST "/moderation/$material/decision" '{"decision":"approved"}';  expect 200 "the moderator approves it again"

token="$author_token"
tag="smoketag$RANDOM"
call POST /tags "{\"name\":\"$tag\"}";   expect 201 "POST /tags"
tag_id="$(jget id)"
call PUT "/notes/$material/tags" "{\"tag_ids\":[\"$tag_id\"]}";  expect 200 "PUT tags (does not unpublish)"
token=""
call GET "/library?tag=$tag";            expect 200 "GET /library?tag="
grep -q "$material" "$tmp/body" || fail "the tag filter lost the material: $(cat "$tmp/body")"
grep -q "\"$tag\"" "$tmp/body" || fail "the card must carry the tag"
call GET "/library?tag=nosuchtag$RANDOM"; expect 200 "an unknown tag"
grep -q '"items":\[\]' "$tmp/body" || fail "an unknown tag must give an empty page"
call GET "/library/$material";           expect 200 "a guest reads the republished material"
author_id="$(jget author.id)"
call GET "/authors/$author_id";          expect 200 "GET /authors/{id} (public profile)"
[ "$(jget materials_count)" -ge 1 ] || fail "the profile must count the material"
call GET "/library?author_id=$author_id"; expect 200 "GET /library?author_id="
grep -q "$material" "$tmp/body" || fail "the author filter lost the material"
call GET "/ranking/authors?limit=100";   expect 200 "GET /ranking/authors"
grep -q "$author_id" "$tmp/body" || fail "the author is missing from the ranking"
[ "$(jget items.0.rank)" = "1" ] || fail "the ranking must start at place 1"
call GET "/authors/00000000-0000-4000-8000-000000000000"; expect 404 "an unknown author"

token="$reader_token"
call PATCH "/comments/$comment_id" '{"body":"Thanks, this really helped!"}';  expect 200 "the reader edits their comment"
[ -n "$(jget edited_at)" ] || fail "edited_at must be set: $(cat "$tmp/body")"
token="$author_token"
call PATCH "/comments/$comment_id" '{"body":"hijacked"}';  expect 404 "the author cannot edit someone else's comment"

token="$author_token"
call POST "/moderation/$material/unpublish" '{"reason":"nope"}';       expect 403 "a student cannot take material down"
token="$moderator_token"
call POST "/moderation/$material/unpublish" '{"reason":"   "}';        expect 400 "a take-down needs a reason"
call POST "/moderation/$material/unpublish" '{"reason":"Contains an error in the main formula"}';  expect 200 "the moderator takes the material down"
token=""
call GET "/library/$material";           expect 404 "a taken-down material leaves the library"
token="$author_token"
call GET "/notes/$material";             expect 200 "the author still sees it"
[ "$(jget status)" = "rejected" ] || fail "a taken-down material is rejected"
[ "$(jget rejection_reason)" = "Contains an error in the main formula" ] || fail "the author must see the reason: $(cat "$tmp/body")"

step "password change ends the old password"
call POST /me/password "{\"current_password\":\"$password\",\"new_password\":\"smoke-password-2\"}"
expect 200 "POST /me/password"
token="$(jget access_token)"
call POST /auth/login "{\"email\":\"$email\",\"password\":\"$password\"}";            expect 401 "login with the old password"
call POST /auth/login "{\"email\":\"$email\",\"password\":\"smoke-password-2\"}";    expect 200 "login with the new password"


step "password reset by e-mail (the mail is read from Mailpit)"
mailpit="${MAILPIT_URL:-http://localhost:8025}"
forgetful="forgetful-$(date +%s)-$RANDOM@example.com"
token=""
call POST /auth/register "{\"email\":\"$forgetful\",\"password\":\"forgotten-pass-1\",\"display_name\":\"Forgetful\"}"; expect 201 "another student registers"
call POST /auth/password-reset "{\"email\":\"nobody-$RANDOM@example.com\"}";  expect 204 "an unknown address gets the same answer"
call POST /auth/password-reset "{\"email\":\"$forgetful\"}";                  expect 204 "a known address gets the same answer"
reset_token=""
for _ in $(seq 1 20); do   # письмо уходит в фоне
  curl -sS -o "$tmp/body" "$mailpit/api/v1/search?query=to:$forgetful" || true
  mail_id="$(jget messages.0.ID 2>/dev/null || true)"
  if [ -n "$mail_id" ]; then
    curl -sS -o "$tmp/body" "$mailpit/api/v1/message/$mail_id"
    reset_token="$(jget Text | perl -ne 'print $1 if /token=([A-Za-z0-9_-]+)/')"
    [ -n "$reset_token" ] && break
  fi
  sleep 0.5
done
[ -n "$reset_token" ] || fail "no reset link arrived in Mailpit ($mailpit)"
echo "ok  the reset link arrived by mail"
call POST /auth/password-reset/confirm "{\"token\":\"$reset_token\",\"new_password\":\"remembered-pass-2\"}";  expect 204 "POST /auth/password-reset/confirm"
call POST /auth/password-reset/confirm "{\"token\":\"$reset_token\",\"new_password\":\"remembered-pass-3\"}";  expect 401 "the link works once"
call POST /auth/login "{\"email\":\"$forgetful\",\"password\":\"forgotten-pass-1\"}";   expect 401 "login with the forgotten password"
call POST /auth/login "{\"email\":\"$forgetful\",\"password\":\"remembered-pass-2\"}";  expect 200 "login with the new password"

step "account deletion removes the data and the stored file"
token="$author_token"
call DELETE /me '{"password":"not-my-password"}';  expect 403 "a wrong password deletes nothing"
call GET /me;                                      expect 200 "the account is intact"
call DELETE /me '{"password":"smoke-password-2"}'; expect 204 "DELETE /me"
call GET /me;                                      expect 401 "the access token of a deleted account stops working"
call POST /auth/login "{\"email\":\"$email\",\"password\":\"smoke-password-2\"}";  expect 401 "a deleted account cannot sign in"
token=""
call GET "/library/$material";                     expect 404 "its materials are gone"
gone="$(curl -sS -o /dev/null -w '%{http_code}' "$download_url")"
[ "$gone" = "404" ] || fail "the uploaded file must be removed from storage, the old download link answered $gone"
echo "ok  the stored file is gone from S3 (404)"

printf '\nAll smoke checks passed.\n'
