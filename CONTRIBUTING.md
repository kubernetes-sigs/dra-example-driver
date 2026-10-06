# Contributing

Issues and pull requests are welcome at
<https://github.com/soer3n/kube-ovn-dra-driver>.

## Development

- `make test` runs the unit tests and `logcheck`; `make test-privileged` runs
  the `pkg/plumbing` datapath tests in network namespaces (needs root).
- `make lint` runs `golangci-lint`; `make assert-fmt` checks formatting.
- `make setup-e2e test-e2e teardown-e2e` runs the e2e suite in kind; see
  [`docs/nic-driver.md`](docs/nic-driver.md) for the demo cluster.
- Run `make generate` after changing the API types in `api/`.

The driver depends on kube-ovn with `--enable-dra-nic`; changes to the contract
between both (pod annotations, device attributes, port names) must stay in sync
with kube-ovn's `docs/dra-nic.md`.

## Pull requests

- Keep commits focused and sign them off (`git commit -s`), certifying the
  [Developer Certificate of Origin](https://developercertificate.org/).
- Add or update tests with every change in behavior.
- New files carry the Apache 2.0 license header used in the repository.

Participation is governed by the [code of conduct](code-of-conduct.md).
