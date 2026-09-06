#!/usr/bin/env bash
# Submit every sitemap URL to IndexNow (Bing, Yandex, Naver, Seznam share it), which
# needs no account: the key file at the site root proves ownership. Run after a
# deploy has landed; 200 or 202 means accepted, 422 means the key file is not yet
# served, 429 means slow down. Idempotent: resubmitting is harmless.
set -euo pipefail
HOST="kolkrabbi.francomichetti.com"
KEY="c7bac75b25de520e644e5d92d96e4473"
SITEMAP="site/sitemap.xml"
urls=$(grep -o '<loc>[^<]*</loc>' "$SITEMAP" | sed 's/<\/\?loc>//g' | awk '{printf "%s\"%s\"", (NR>1?",":""), $0}')
body=$(printf '{"host":"%s","key":"%s","keyLocation":"https://%s/%s.txt","urlList":[%s]}' "$HOST" "$KEY" "$HOST" "$KEY" "$urls")
code=$(curl -sS -o /dev/null -w "%{http_code}" -H "Content-Type: application/json; charset=utf-8" -d "$body" "https://api.indexnow.org/IndexNow")
echo "indexnow: HTTP $code"
case "$code" in 200|202) exit 0;; *) exit 1;; esac
