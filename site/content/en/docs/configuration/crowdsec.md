---
title: CrowdSec and Traefik
weight: 35
---

CrowdSec is an optional central decision API for the admin node. It is disabled
by default. When enabled, Traefik downloads the pinned bouncer plugin and checks
every HTTPS request against the LAPI decision stream.

## Request paths

The deployment deliberately separates direct and tunneled traffic:

```text
LAN / direct client ----> host :443 ----> Traefik websecure ---------+
                                                                    |
Cloudflare ----> cloudflared ----> Traefik :8443 cloudflarewebsecure +--> CrowdSec middleware --> service
                    |                       (internal network only)
                    +----> Cloudflare edge (dedicated egress network)

Traefik bouncer ----------------> crowdsec:8080 (private network)
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
    cloudflared_ip: "172.31.255.2"

crowdsec:
  enabled: true
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
`cloudflared_ip` inside that subnet and do not assign it to another container.
Changing either value causes Docker network reconciliation and may briefly
interrupt tunnel traffic during convergence.

`crowdsec.lapi.allowed_cidrs` controls who may call the HTTPS LAPI hostname. It
must not be confused with `traefik.forwarded_headers_trusted_ips`, which lists
additional reverse proxies allowed in front of the direct HTTPS entrypoint.
Never add arbitrary client or LAN ranges merely to make forwarded headers work.

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
```

CrowdSec participates in optional-stack cleanup, systemd startup ordering, and
restore. Backups include its persistent configuration and decision data. After
a restore, convergence reuses the node key, reconciles bouncer registration,
and republishes generated credentials to OpenBao.
