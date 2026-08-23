#!/usr/bin/env bash
# Independent contract for the deploy-ready static landing page.
set -uo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SITE="$ROOT/site"
DEPLOY_DOC="$ROOT/docs/cloudflare-pages.md"
failures=0
checks=0

pass() { checks=$((checks + 1)); }
fail() { checks=$((checks + 1)); failures=$((failures + 1)); printf 'site: %s\n' "$1" >&2; }

require_file() {
  if [ -f "$SITE/$1" ]; then pass; else fail "missing $1"; fi
}

contains() {
  local file="$1" text="$2" label="$3"
  if [ -f "$SITE/$file" ] && grep -Fq -- "$text" "$SITE/$file"; then pass; else fail "$label"; fi
}

contains_deploy_doc() {
  local text="$1" label="$2"
  if [ -f "$DEPLOY_DOC" ] && grep -Fq -- "$text" "$DEPLOY_DOC"; then pass; else fail "$label"; fi
}

excludes() {
  local file="$1" pattern="$2" label="$3"
  if [ -f "$SITE/$file" ] && ! grep -Eiq "$pattern" "$SITE/$file"; then pass; else fail "$label"; fi
}

for file in index.html styles.css logo.svg favicon.svg 404.html _headers robots.txt; do
  require_file "$file"
done

contains index.html '<html lang="en">' "index.html must declare its language"
contains index.html 'name="viewport"' "index.html must configure a mobile viewport"
contains index.html '<main' "index.html must have a semantic main region"
contains index.html 'https://kolkrabbi.francomichetti.com/install.sh' "install URL drifted"
contains index.html 'kolk key &lt;API_KEY&gt;' "API-key command drifted"
contains index.html 'Installer ships with v0.1' "pre-release installer status is missing"
contains index.html 'https://github.com/onembyte/kolkrabbi' "GitHub link is wrong"
contains index.html 'Apache-2.0 License' "license link or label does not match LICENSE"
excludes index.html '<script([ >])' "landing page must not ship JavaScript"
excludes index.html "<(script|img|link)[^>]+(src|href)=[\"']https?://" "landing page loads an external resource"
excludes index.html "style=[\"']" "styles must stay in styles.css for a strict CSP"

contains styles.css '--violet:' "purple palette token is missing"
contains styles.css '@media (max-width:' "responsive layout rule is missing"
contains styles.css ':focus-visible' "keyboard focus style is missing"
contains styles.css 'prefers-reduced-motion' "reduced-motion support is missing"
excludes styles.css "@import|url\\([\"']?https?://" "CSS loads an external dependency"

contains logo.svg 'role="img"' "logo needs an image role"
contains logo.svg 'aria-labelledby=' "logo needs an accessible name"
contains logo.svg 'shape-rendering="crispEdges"' "logo is not explicitly pixel-rendered"
contains logo.svg '#a78bfa' "logo is missing the purple highlight"
contains favicon.svg '<svg' "favicon must be an SVG"

for header in Content-Security-Policy X-Frame-Options X-Content-Type-Options Referrer-Policy Permissions-Policy; do
  contains _headers "$header:" "_headers is missing $header"
done
contains _headers '/install.sh' "_headers has no installer-specific policy"
contains _headers 'Cache-Control: no-store' "installer must not be cached across releases"
contains _headers 'Content-Type: text/plain; charset=utf-8' "installer MIME policy is missing"

contains_deploy_doc '| Production branch | `main` |' "Pages production branch is not documented"
contains_deploy_doc '| Build command | `exit 0` |' "Pages build command is not documented"
contains_deploy_doc '| Build output directory | `site` |' "Pages output directory is not documented"
contains_deploy_doc 'kolkrabbi.francomichetti.com' "custom hostname is not documented"
contains_deploy_doc 'wildcard multitenant fallback' "wildcard fallback ownership is not documented"
contains_deploy_doc 'must remain unchanged' "wildcard preservation rule is not documented"
contains_deploy_doc 'TrueNAS multitenant origin' "TrueNAS wildcard origin is not documented"
contains_deploy_doc 'Do not begin by manually creating a CNAME' "safe custom-domain ordering is missing"

if [ "$failures" -ne 0 ]; then
  printf 'site: %d/%d checks failed\n' "$failures" "$checks" >&2
  exit 1
fi
printf 'site: %d checks passed\n' "$checks"
