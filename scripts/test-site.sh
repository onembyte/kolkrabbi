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
  local result
  if [ ! -f "$SITE/$file" ]; then
    fail "$label"
  elif grep -Eiq "$pattern" "$SITE/$file"; then
    fail "$label"
  else
    result=$?
    if [ "$result" -eq 1 ]; then pass; else fail "$label (invalid exclusion check)"; fi
  fi
}

provider_status() {
  local name="$1" want="$2" got
  got="$(grep -B3 -F ">$name</span>" "$SITE/index.html" | grep -o 'data-support="[a-z]*"' | tail -n 1)"
  if [ "$got" = "data-support=\"$want\"" ]; then pass; else fail "$name must be marked $want on the provider wall"; fi
}

last_section_is() {
  local file="$1" id="$2" found
  found="$(grep -Eo '<section[^>]+id="[^"]+"' "$SITE/$file" | tail -n 1)"
  if [[ "$found" == *"id=\"$id\""* ]]; then pass; else fail "$id must be the last main-content section"; fi
}

for file in index.html capabilities.html styles.css app.js logo.svg favicon.svg 404.html _headers robots.txt; do
  require_file "$file"
done

contains index.html '<html lang="en">' "index.html must declare its language"
contains index.html 'name="viewport"' "index.html must configure a mobile viewport"
contains index.html '<main' "index.html must have a semantic main region"
contains index.html 'https://kolkrabbi.francomichetti.com/install.sh' "install URL drifted"
contains index.html 'kolk key &lt;API_KEY&gt;' "API-key command drifted"
contains index.html '<code class="key-command"><span class="prompt" aria-hidden="true">$</span> kolk key &lt;API_KEY&gt;</code>' "API-key command is not in the run step"
contains index.html '<code class="use-command"><span class="prompt" aria-hidden="true">$</span> kolk</code>' "use step must contain only the final kolk command"
contains index.html 'Installer ships with v1.2.20' "current installer release status is missing"
contains index.html 'https://github.com/onembyte/kolkrabbi' "GitHub link is wrong"
contains index.html 'Apache-2.0 License' "license link or label does not match LICENSE"
contains index.html 'Chat, code, and agent' "landing page does not name all three modes"
contains index.html 'Three modes' "landing page does not present the three-mode surface"
contains index.html 'agent for longer work' "landing page does not explain when to use agent mode"
contains index.html 'class="nav-button" href="/capabilities.html"' "landing page has no capabilities navbar button"
contains index.html 'id="install-command"' "install command has no copy target"
contains index.html 'class="copy-button" type="button" data-copy-target="install-command"' "install command has no copy button"
contains index.html 'class="copy-icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false"' "copy button has no decorative copy icon"
contains index.html 'id="copy-status" role="status" aria-live="polite"' "copy result is not announced accessibly"
contains index.html '<script src="app.js" defer></script>' "landing page does not load the local copy controller"
excludes index.html 'parallel subagents|subagents in parallel|at once' "landing page inaccurately claims parallel orchestration"
excludes index.html '<script[^>]*>[^<]' "landing page must not ship inline JavaScript"
excludes index.html "<(script|img|link)[^>]+(src|href)=[\"']https?://" "landing page loads an external resource"
excludes index.html "style=[\"']" "styles must stay in styles.css for a strict CSP"

contains index.html 'id="providers"' "landing page has no provider wall"
contains index.html 'class="provider-wall"' "provider wall markup is missing"
contains index.html '<span class="provider-mark" aria-hidden="true"><svg viewBox="0 0 24 24"' "provider marks must be inline SVG, not fetched images, for a strict CSP"
# A tile is a promise. Lit means an adapter exists in this repository today;
# every other provider on docs/plan/24-subscription-provider-matrix.md is dim.
provider_status OpenRouter live
provider_status Anthropic live
provider_status Ollama live
provider_status LiteLLM live
provider_status vLLM live
provider_status OpenAI planned
provider_status Google planned
provider_status xAI planned
provider_status Perplexity planned
provider_status Mistral planned
provider_status DeepSeek planned
provider_status Qwen planned
provider_status Cohere planned
provider_status GitHub planned

# The checks above are written by hand, which means a provider added to the
# repository could sit there unlisted while every one of them still passed.
# This derives the expected set from internal/provider/plans.go instead: every
# provider kolk knows a plan for must have a tile, so the wall cannot silently
# fall behind the code.
#
# The mapping is display names, because a wall reads "Anthropic" and a table
# says "anthropic"; anything new must be added here on purpose, which is the
# point — a provider arriving in the repo should make somebody decide what the
# page says about it.
# A plain list rather than an associative array: macOS ships bash 3.2, which
# has no `declare -A`, and under `set -u` it failed as "anthropic: unbound
# variable" — so this guard passed in CI on Linux and could not run at all on
# the machine the releases are cut from.
provider_tiles="anthropic=Anthropic openai=OpenAI google=Google xai=xAI
perplexity=Perplexity mistral=Mistral deepseek=DeepSeek qwen=Qwen
cohere=Cohere github=GitHub ollama=Ollama"

tile_for() {
  local wanted="$1" pair
  for pair in $provider_tiles; do
    if [ "${pair%%=*}" = "$wanted" ]; then
      printf '%s\n' "${pair#*=}"
      return 0
    fi
  done
  return 1
}

while read -r provider; do
  tile="$(tile_for "$provider" || true)"
  if [ -z "$tile" ]; then
    fail "internal/provider/plans.go knows the provider $provider, which has no tile on the wall and no name mapped for it"
    continue
  fi
  contains index.html "provider-name\">$tile<" "$provider is in plans.go but has no tile on the provider wall"
done < <(grep -oE '\{Provider: "[a-z]+"' "$ROOT/internal/provider/plans.go" | sed 's/.*"\([a-z]*\)"/\1/' | sort -u)

contains capabilities.html '<html lang="en">' "capabilities page must declare its language"
contains capabilities.html 'name="viewport"' "capabilities page must configure a mobile viewport"
contains capabilities.html '<main id="content">' "capabilities page must have a semantic main region"
contains capabilities.html 'class="nav-button" href="/capabilities.html" aria-current="page"' "capabilities navbar state is missing"
contains capabilities.html 'Available now' "capability status legend is missing the working state"
contains capabilities.html 'Designed, not shipped' "capability status legend is missing the designed state"
contains capabilities.html 'Planned' "capability status legend is missing the planned state"
contains capabilities.html 'data-status="available"' "catalog has no available capability rows"
contains capabilities.html 'data-status="designed"' "catalog has no designed capability rows"
contains capabilities.html 'data-status="planned"' "catalog has no planned capability rows"

# A capability claimed here has to be one the binary has. These four shipped on
# 2026-08-27 and the page says so, which makes each line a promise the suite is
# responsible for. MCP is deliberately still Planned: its permission rules and
# schema budget exist, its transports do not.
contains capabilities.html '>LOOP GUARD<' "catalog does not mention the doom-loop guard"
contains capabilities.html '>DOCTOR<' "catalog does not mention kolk doctor and --debug"
contains capabilities.html '>COMMANDS<' "catalog does not mention markdown commands"
contains capabilities.html '>HOOKS<' "catalog does not mention hooks and their confirmation"
excludes capabilities.html 'status-badge">Available now</span><span>MCP<' "MCP is claimed as shipped; only its permission rules and schema budget are"

# The reach section is a server surface with no first-party client, and it read
# as a phone app until someone said so. These two lines keep the distinction:
# the missing half must stay named, and the answer-a-prompt card must not
# describe a device experience that does not exist.
contains capabilities.html '>CLIENT<' "the missing remote client is not named, so the reach section reads as a finished feature"
excludes capabilities.html 'A paired device follows the live event stream' "the reach section describes a device experience that has no client"
for section in working access continuity workflows safety interfaces reach videos; do
  contains capabilities.html "id=\"$section\"" "capabilities page is missing the $section section"
done
contains capabilities.html 'Chat, code, and agent modes' "catalog does not cover all three modes"
contains capabilities.html 'OpenRouter works today' "catalog does not distinguish the working model path"
contains capabilities.html 'supported API key' "catalog does not cover provider-agnostic key onboarding"
contains capabilities.html 'Claude Agent subscription' "catalog does not cover Claude subscription sign-in"
contains capabilities.html 'Codex subscription' "catalog does not cover Codex subscription sign-in"
contains capabilities.html 'same Kolkrabbi session' "catalog does not cover cross-backend session continuity"
contains capabilities.html 'subscription limit' "catalog does not cover subscription-cap handling"
contains capabilities.html 'best-rated eligible configured model' "catalog does not define the continuation choice"
contains capabilities.html 'Ask before free fallback' "catalog does not state the safe free-fallback default"
contains capabilities.html 'Automatic switching' "catalog does not state the opt-in automatic policy"
contains capabilities.html 'Themes' "catalog does not cover theme choices"
contains capabilities.html 'Orchestrated agent runs' "catalog does not cover the shipped orchestrator"
# Self-update shipped as U0.2a-d and was the one capability the catalog forgot,
# found by an audit comparing `kolk help` against the page rather than trusting
# the release checkpoint that said the two were in line.
contains capabilities.html 'kolk update' "catalog does not cover self-update"
contains capabilities.html 'Careful long-running progression' "catalog does not cover the saga loop"
contains capabilities.html 'Permission rules and path jail' "catalog does not cover permission rules"
contains capabilities.html 'Local model dashboard' "catalog does not cover the local dashboard"
contains capabilities.html 'Attachable local service' "catalog does not cover the event service"
contains capabilities.html 'One revocable token per device' "catalog does not cover per-device tokens"
# The uninstall path and the question picker shipped in v1.2.17 and v1.2.16. The
# first is pinned because someone looking for it is usually already frustrated
# and will not go hunting; the second because a capability nobody is told about
# is one the product does not have, which is the mistake v1.2.15 made by
# shipping the picker with nothing inviting the model to use it.
contains index.html 'kolk uninstall' "the install steps do not say how to leave"
contains index.html 'kolk uninstall --keep-data' "the uninstall step does not say how to keep a key for a reinstall"
contains capabilities.html 'Leaving is one command too' "catalog does not cover uninstall"
contains capabilities.html 'A question you answer by picking' "catalog does not cover the question picker"
# Six, not five: ask_user joined the tool set and the schema of every tool is
# sent on every request of every turn, so the count is a cost the page states.
contains capabilities.html 'Six focused tools' "catalog miscounts the tools it sends"
excludes capabilities.html 'Five focused tools' "catalog still claims the pre-ask_user tool count"
excludes capabilities.html 'yolo' "catalog names a permission surface that no longer exists"
contains capabilities.html 'data-language="en"' "English explainer slot is missing"
contains capabilities.html 'data-language="es"' "Spanish explainer slot is missing"
contains capabilities.html 'English explainer' "English explainer label is missing"
contains capabilities.html 'Explicación en español' "Spanish explainer label is missing"
contains capabilities.html 'Coming soon' "video placeholders are not honest about availability"
contains capabilities.html 'Próximamente' "Spanish video placeholder is not honest about availability"
last_section_is capabilities.html videos
excludes capabilities.html 'claude code' "catalog ships the prohibited Claude Code product or feature name"
excludes capabilities.html '<script([ >])' "capabilities page must not ship JavaScript"
excludes capabilities.html '<(iframe|video|source)([ >])' "capabilities page ships media before real sources exist"
excludes capabilities.html "<(script|img|link)[^>]+(src|href)=[\"']https?://" "capabilities page loads an external resource"
excludes capabilities.html "style=[\"']" "capabilities styles must stay in styles.css for a strict CSP"

contains styles.css '--violet:' "purple palette token is missing"
contains styles.css '.nav-button' "capabilities navbar button style is missing"
contains styles.css '.copy-button' "install copy button style is missing"
contains styles.css '.copy-icon' "install copy icon style is missing"
contains styles.css '.command-row' "install command/button layout is missing"
contains styles.css '.key-command' "run-step key command layout is missing"
contains styles.css '.status-badge' "capability status style is missing"
contains styles.css '.video-grid' "bilingual video layout style is missing"
contains styles.css '.provider-wall' "provider wall layout style is missing"
contains styles.css '[data-support="planned"]' "unsupported providers have no dimmed style"
contains styles.css '@media (max-width:' "responsive layout rule is missing"
contains styles.css ':focus-visible' "keyboard focus style is missing"
contains styles.css 'prefers-reduced-motion' "reduced-motion support is missing"
excludes styles.css "@import|url\\([\"']?https?://" "CSS loads an external dependency"

contains app.js 'navigator.clipboard.writeText' "copy controller does not use the Clipboard API"
contains app.js 'document.execCommand("copy")' "copy controller has no compatibility fallback"
contains app.js 'Install command copied to clipboard.' "copy controller has no accessible success message"
excludes app.js 'https?://|eval\(|innerHTML' "copy controller uses an unsafe or external primitive"

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
contains _headers "script-src 'self'" "CSP does not allow only the local copy controller"

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
