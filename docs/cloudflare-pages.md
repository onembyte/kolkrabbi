# Cloudflare Pages handoff

This is the owner-operated deployment contract for the static files in [`site/`](../site/). The
repository does not contain a Cloudflare token and CI does not mutate DNS.

## Current live state

Initially observed on 2026-08-23, before the owner completed the cutover:

- `kolkrabbi.francomichetti.com` already resolves through Cloudflare.
- `/` returns `302` to `/login`, then a cached Next.js HTML page.
- `/install.sh` returns the same redirect and HTML, not a shell script.
- The owner confirmed that this is the `*.francomichetti.com` wildcard multitenant fallback, not an
  existing Kolkrabbi application or an exact `kolkrabbi` binding.
- The wildcard ultimately serves the owner's TrueNAS multitenant origin.

The owner then connected the Pages project to `main`, created the exact `kolkrabbi` CNAME through
the custom-domain flow, and reported the hostname **Active**. A direct HTTPS check returned the
reviewed octopus page with HTTP 200 and its security headers. The wildcard and TrueNAS origin remain
the fallback for other, unclaimed subdomains.

The wildcard multitenant fallback must remain unchanged. Cloudflare gives an exact DNS record
precedence over a wildcard record, so the cutover adds an exact `kolkrabbi` Pages binding and does
not move or replace the personal site. Do not advertise or run the public install command until the
release assets exist and the clean-machine rehearsal passes. T0.4b may deploy the reviewed script
before then, but it fails closed when no public release can be downloaded.

The intended request split is:

| Request | Cloudflare binding | Origin |
|---|---|---|
| `kolkrabbi.francomichetti.com` | exact `kolkrabbi` CNAME | Cloudflare Pages |
| any otherwise-unclaimed subdomain | existing `*` fallback | TrueNAS multitenant origin |

This design requires no TrueNAS application, reverse-proxy, port-forwarding, or Tunnel ingress
change. The exact Pages record prevents Kolkrabbi requests from reaching the wildcard origin.

## Pages project

In **Cloudflare dashboard → Workers & Pages → Create application → Pages → Import an existing Git
repository**, select `onembyte/kolkrabbi` and use:

| Setting | Value |
|---|---|
| Project name | `kolkrabbi-site` (or another available name) |
| Production branch | `main` |
| Framework preset | None |
| Root directory | `/` |
| Build command | `exit 0` |
| Build output directory | `site` |
| Environment variables | None |

Deploy and review the generated `https://<project>.pages.dev` preview before touching the custom
hostname. No Pages Functions, Worker route, KV, R2, D1, Access policy, analytics script, or package
install is needed.

## Custom-domain cutover

1. In the new Pages project, open **Custom domains → Set up a domain**.
2. Enter `kolkrabbi.francomichetti.com` there first. Do not begin by manually creating a CNAME;
   Cloudflare documents that doing so before Pages knows the hostname can produce a 522.
3. The wildcard DNS record is not a conflict. The wizard should create an exact, proxied CNAME
   named `kolkrabbi` targeting the exact `<project>.pages.dev` hostname. If an exact `kolkrabbi`
   record unexpectedly exists, stop and inspect it; do not change the `*` record.
4. In **francomichetti.com → DNS → Records**, confirm these two independent records:

   | Type | Name | Target | Purpose |
   |---|---|---|---|
   | Existing wildcard | `*` | existing TrueNAS tunnel/origin target | every unclaimed subdomain |
   | Proxied CNAME | `kolkrabbi` | `<project>.pages.dev` | Kolkrabbi only |

   The existing wildcard row, its TrueNAS multitenant target, and its proxy status must remain
   unchanged.
5. The final Kolkrabbi DNS shape is therefore a proxied CNAME named `kolkrabbi` targeting the exact
   `<project>.pages.dev` hostname Cloudflare assigned. When the zone and Pages project are in the
   same account, the wizard normally creates this record automatically.
6. Check **Workers & Pages → Workers → Routes**. If there is no wildcard Worker route, make no
   change. If a route such as `*.francomichetti.com/*` sends every subdomain through the
   multitenant Worker, add the more-specific route `kolkrabbi.francomichetti.com/*` with **no
   Worker/script**. Cloudflare documents that a route without a script negates a less-specific
   Worker route. This exception is unnecessary when the multitenancy is only the wildcard DNS row.
7. If a Redirect Rule implements the multitenant redirect, exclude the exact host with
   `http.host ne "kolkrabbi.francomichetti.com"`. Do not change a redirect implemented only inside
   the personal Next.js application; exact DNS prevents requests from reaching that application.
8. Wait for the Pages custom domain to show **Active** and its edge certificate to become valid.
   If certificate issuance stalls, inspect the zone's CAA records before adding or deleting any.

Cloudflare Pages applies [`site/_headers`](../site/_headers) to the static responses. Do not add a
competing Transform Rule or Cache Rule for `/install.sh`; it is deliberately `text/plain` and
`no-store` so a replaced release installer cannot remain in a browser or intermediary cache.

## Verification after cutover

Once the T0.4b script deploys, verify it without executing it:

```sh
curl -fsSI https://kolkrabbi.francomichetti.com/
curl -fsSI https://kolkrabbi.francomichetti.com/install.sh
curl -fsSL https://kolkrabbi.francomichetti.com/install.sh | sh -n
```

The first response must carry the CSP and security headers. The installer must be HTTP 200,
`Content-Type: text/plain`, `Cache-Control: no-store`, and pass `sh -n`. Those checks prove Pages
delivery only; the command remains unavailable for owner testing until a signed public release and
the clean-machine rehearsal also pass.

Primary references: [Cloudflare Pages static HTML](https://developers.cloudflare.com/pages/framework-guides/deploy-anything/),
[custom domains](https://developers.cloudflare.com/pages/configuration/custom-domains/), and
[`_headers`](https://developers.cloudflare.com/pages/configuration/headers/). The separation rules
follow Cloudflare's documented [exact-over-wildcard DNS precedence](https://developers.cloudflare.com/dns/manage-dns-records/reference/wildcard-dns-records/)
and [Worker route matching](https://developers.cloudflare.com/workers/configuration/routing/routes/).
