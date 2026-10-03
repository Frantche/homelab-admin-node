---
title: CrowdSec and Traefik
weight: 35
---

CrowdSec is an optional decision API and log-based detection engine for the
admin node. It is disabled by default. When enabled, Traefik downloads the
pinned bouncer plugin and checks every HTTPS request against the LAPI decision
stream. The log acquisition agent is separately opt-in.

## Request paths

The deployment deliberately separates direct and tunneled traffic:

```text
LAN / direct client ----> host :443 ----> Traefik websecure ---------+
                                                                    |
Cloudflare ----> cloudflared ----> Traefik :8443 cloudflarewebsecure +--> CrowdSec middleware --> service
                    |                       (internal network only)
                    +----> Cloudflare edge (dedicated egress network)

Traefik bouncer ----------------> crowdsec:8080 (private network)
Traefik access log -------------> CrowdSec agent (read-only shared directory)
CrowdSec LAPI ------------------> CAPI (dedicated egress network)
```

Port `443` is the only Traefik port published on the host. Port `8443` is
exposed only inside the `traefik-cloudflared` Docker network. `cloudflared` is
not attached to the general `admin-edge` network: it has the internal origin
network for Traefik and `cloudflare-egress` for outbound tunnel connections.

The `cloudflarewebsecure` entrypoint trusts `X-Forwarded-*` only from the fixed
`cloudflared` container address. The direct `websecure` entrypoint continues to
trust only `traefik.forwarded_headers_trusted_ips`. This prevents a direct
client from impersonating another address while preserving the original
Cloudflare client IP for CrowdSec decisions.

Both HTTPS entrypoints prepend `crowdsec-bouncer@file` to every matching router.
This entrypoint-level attachment protects built-in services, the dashboard,
external services, and future routers without requiring each route to remember
the middleware. The LAPI route is also protected and remains LAN-allowlisted;
there is no request loop because the plugin calls `crowdsec:8080` directly.

## Configuration

Enable the stack in the private configuration repository:

```yaml
cloudflare:
  enabled: true
  origin_network:
    subnet: "172.31.255.0/29"
    traefik_ip: "172.31.255.2"
    cloudflared_ip: "172.31.255.3"

crowdsec:
  enabled: true
  agent:
    enabled: true
    collections:
      - crowdsecurity/traefik
    fast_http_probing:
      enabled: false
      capacity: 3
      leakspeed: "10s"
  remediation:
    ban_duration: "4h"
    recidivism:
      enabled: true
      window: "720h"
  capi:
    enabled: true
  lapi:
    allowed_cidrs:
      - "192.168.1.0/24"
  traefik_bouncer:
    plugin_version: "v1.4.5"
    update_interval_seconds: 60
  openbao:
    mount: "secret"
    path_prefix: "crowdsec"
```

Choose an unused private subnet for `cloudflare.origin_network.subnet` if the
default overlaps another Docker, LAN, VPN, or container network. Keep
`traefik_ip` and `cloudflared_ip` distinct and inside that subnet. Changing
either address recreates the affected container and may briefly interrupt
tunnel traffic during convergence.

`crowdsec.lapi.allowed_cidrs` controls who may call the HTTPS LAPI hostname. It
must not be confused with `traefik.forwarded_headers_trusted_ips`, which lists
additional reverse proxies allowed in front of the direct HTTPS entrypoint.
Never add arbitrary client or LAN ranges merely to make forwarded headers work.

## Automatic HTTP attack detection

Set `crowdsec.agent.enabled: true` to enable log acquisition. Convergence writes
Traefik access logs as JSON to
`/srv/admin/data/traefik/crowdsec-logs/access.log`, mounts that directory
read-only into CrowdSec, and installs the `crowdsecurity/traefik` collection.
The collection includes a Traefik parser and common HTTP scenarios for crawling,
404 scanning, and brute force ([collection details](https://app.crowdsec.net/hub/author/crowdsecurity/collections/traefik)).
Traefik drops request headers and query parameters from these logs. Log rotation
keeps seven compressed files and checks daily for logs larger than 25 MB.

The optional `crowdsec.agent.fast_http_probing` scenario detects distinct HTTP
error paths at a configurable threshold without changing the collection's
upstream scenarios. Enable it only where a lower threshold is wanted; for
example, `capacity: 3` triggers after four distinct matching paths in the
bucket. The bootstrap PR journey enables this scenario with a one-second
Traefik bouncer refresh and a 24-hour ban profile. The DI configuration keeps
the optional scenario disabled and the normal 60-second refresh and four-hour
ban duration.

Set `crowdsec.remediation.ban_duration` to control the base IP and range ban
duration. With recidivism enabled, a base duration in whole hours is required.
CrowdSec accepts Go duration strings such as `30m`, `4h`, or `1h30m` when
recidivism is disabled.
`crowdsec.remediation.recidivism.window` controls how far back CrowdSec counts
prior decisions for the same source. The default `720h` is 30 days. Within that
window, the first ban uses the base duration, the first repeat doubles it, and
each additional repeat adds one more base duration. For a `4h` base, successive
bans are `4h`, `8h`, `12h`, and so on. Set `enabled: false` to keep a fixed
duration.
Traefik's `crowdsec.traefik_bouncer.update_interval_seconds` controls how often
the stream-mode bouncer fetches decisions from the LAPI; reducing it shortens
the delay between a decision and enforcement, but does not change scenario
detection thresholds.

Check that CrowdSec is reading and parsing the access log and that scenarios
are loaded:

```bash
docker exec crowdsec cscli metrics show acquisition parsers scenarios
docker exec crowdsec cscli collections list | grep crowdsecurity/traefik
docker exec crowdsec cscli alerts list
sudo tail -n 50 /srv/admin/data/traefik/crowdsec-logs/access.log
```

CrowdSec's default allowlist ignores loopback and private LAN source addresses
to avoid banning local clients. Test automatic detections from an external
client through the public hostname; inspect alerts and decisions before
removing a test decision. This log-based setup detects the patterns covered by
the installed scenarios. It is not a WAF and does not inspect request bodies.

## Secrets and generated credentials

No bouncer key belongs in Git. During convergence, the node generates the
Traefik key at `/srv/admin/env/crowdsec-traefik-bouncer-key` with mode `0600`,
registers it in the LAPI, and publishes the following values to the configured
OpenBao KV-v2 mount:

Traefik mounts that file read-only and references it with
`crowdsecLapiKeyFile`; the key is never rendered inline in dynamic
configuration.

| Path below `<mount>/<path_prefix>` | Content |
| --- | --- |
| `bouncers/traefik` | Bouncer name, API key, and HTTPS LAPI URL. |
| `capi` | CrowdSec Central API credentials when CAPI is enabled. |
| `lapi/machine` | Local machine credentials when CrowdSec creates them. |

Give every additional bouncer its own API key. Do not copy the Traefik key to
Talos, Kubernetes, or another host. Restrict OpenBao policies to only the path
required by each consumer.

## Optional Web UI

Enable the Web UI through `crowdsec_web_ui` in the private configuration
repository. Put its LAPI password and shared OIDC client secret in
`group_vars/secrets.sops.yaml` as `vault_crowdsec_web_ui_lapi_password` and
`vault_oidc_crowdsec_web_ui_client_secret`. Convergence stores them as
root-owned files readable only by a dedicated group used by this container.
Keep those permissions; do not make the files world-readable. Convergence
manages the confidential Keycloak client from `oidc_clients.crowdsec_web_ui`.
Keep the UI client ID and secret sourced from that shared entry, and declare
`https://<crowdsec_web_ui.hostname>/api/auth/oidc/callback` as its redirect URI
under `keycloak_config.clients`. Convergence keeps the Keycloak client secret
aligned with the secret mounted into the UI so the authorization code exchange
can complete. Users must also belong to a group listed in
`crowdsec_web_ui.oidc.admin_groups` or a configured read-only group. The example
uses `harbor-admins`, matching the example Keycloak user's group membership.

The container runs directly as the image's non-root `node` user. Convergence
assigns the persistent application directory to that runtime UID and starts
the systemd stack unit so a previously failed unit is recovered. Check the
container health and local health endpoint after convergence:

```bash
docker inspect --format '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{end}}' crowdsec-web-ui
docker exec crowdsec-web-ui node -e "fetch('http://127.0.0.1:3000/api/health').then(r => process.exit(r.ok ? 0 : 1)).catch(() => process.exit(1))"
systemctl status admin-stack@crowdsec-web-ui.service
```

The Web UI stays on the isolated `traefik-crowdsec` network. Traefik publishes
the Keycloak issuer hostname as a Docker network alias on that network, so the
UI can fetch OIDC discovery and token endpoints through Traefik without an
internet egress network. When Traefik uses its local TLS certificate, the
configured UI hostname is included in the certificate and a hostname change
renews it during convergence. The UI also trusts the local CA in that mode so
its server-side OIDC discovery and token requests can reach Keycloak. Keep
`crowdsec_web_ui.oidc.issuer_url` aligned with `service_domains.keycloak`.
Check the login endpoint returns a redirect or a success response rather than
an internal server error:

```bash
curl --silent --show-error --output /dev/null --write-out 'HTTP %{http_code}\n' \
  "https://<crowdsec_web_ui.hostname>/api/auth/oidc/login"
```

This uses the host trust store when Traefik serves a public certificate. If
`traefik.local_tls_enabled` is enabled, add
`--cacert /srv/admin/certs/ca.pem` to trust the generated local CA.

## Verify enforcement

After convergence, first confirm the containers and networks:

```bash
docker ps --filter name=traefik --filter name=crowdsec --filter name=cloudflared
docker inspect -f '{{json .NetworkSettings.Networks}}' cloudflared | jq .
docker inspect -f '{{json .NetworkSettings.Networks}}' traefik | jq .
docker exec crowdsec cscli bouncers list
```

The Cloudflare origin routes must target port `8443`, while direct local access
continues to use `443`:

```bash
sudo grep -n 'https://traefik:8443' /srv/admin/stacks/cloudflared/config.yml
sudo grep -A8 -n 'cloudflarewebsecure:' /srv/admin/stacks/traefik/traefik.yml
```

To test a decision, use a disposable client address that you can reach from a
separate terminal. Do not ban the administration address used for SSH:

```bash
docker exec crowdsec cscli decisions add --ip 192.0.2.50 --duration 2m --type ban
docker exec crowdsec cscli decisions list
docker exec crowdsec cscli decisions delete --ip 192.0.2.50
```

Test both the local hostname and the public tunnel hostname from the relevant
client. A blocked request returns HTTP `403`. Traefik access logs should show
the real client address, not the `cloudflared` container address.

## Failure and maintenance behavior

The bouncer runs in stream mode and caches decisions locally. With
`updateMaxFailure: -1`, an unavailable LAPI does not make every application
unavailable; Traefik continues with the last usable state. Investigate the LAPI
instead of restarting all services:

```bash
docker logs --tail 200 crowdsec
docker logs --tail 200 traefik
docker exec crowdsec cscli metrics
docker exec crowdsec cscli decisions list
sudo tail -n 200 /srv/admin/data/traefik/crowdsec-logs/access.log
```

CrowdSec participates in optional-stack cleanup, systemd startup ordering, and
restore. Backups include its persistent configuration and decision data. After
a restore, convergence reuses the node key, reconciles bouncer registration,
and republishes generated credentials to OpenBao.
