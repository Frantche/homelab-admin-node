---
title: Documentation Workflow
weight: 20
---

Documentation source lives under `site/content/en/docs`.

Rules:

- Keep the README short and link to the site for details.
- Put complete procedures in the Hugo docs.
- Update the quickstart only when the common path changes.
- Update configuration pages when variables or examples change.
- Include commands that users can copy and adapt.
- Do not place real secrets, real tokens, or private hostnames in examples.

Build docs locally:

```bash
make docs-build
```

Serve locally:

```bash
make docs-serve
```
