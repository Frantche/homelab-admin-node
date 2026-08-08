## Change

Describe the operator-visible outcome and the reason for the change.

## Lifecycle impact

- [ ] `locked`
- [ ] `init`
- [ ] `normal`
- [ ] `restore` / `restore_failed`
- [ ] CI only

## Security and compatibility

- [ ] No real secret or private key is included
- [ ] Secret-bearing Ansible tasks use `no_log: true`
- [ ] Existing system paths and config-repository interfaces remain compatible
- [ ] Documentation and examples are updated

## Validation

- [ ] `make ci-quality`
- [ ] `make docs-check`
- [ ] Relevant integration journey, or an explanation of why it was not run
