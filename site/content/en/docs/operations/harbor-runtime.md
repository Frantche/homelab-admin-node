---
title: Harbor runtime
weight: 35
---

All Harbor services use Docker's `unless-stopped` restart policy. Docker starts
them after a daemon or host restart and restarts a container when its process
exits. An operator's explicit stop remains in effect until the service is
started again.

The policy reacts to container exits; an unhealthy healthcheck alone does not
restart a process that is still running. A restart also cannot resolve an
underlying issue such as a full data volume, so check the Harbor and host logs
if a service repeatedly exits.
