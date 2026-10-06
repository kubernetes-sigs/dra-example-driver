# Release Process

The kube-ovn NIC DRA driver is released on an as-needed basis. Published
release artifacts are:

- Container images: `docker.io/soer3n/kube-ovn-dra-driver`
- The `kube-ovn-dra-driver` Helm chart: `oci://registry-1.docker.io/soer3n/kube-ovn-dra-driver`

## Container images

`.github/workflows/image.yaml` builds the image for `linux/amd64` and
`linux/arm64` on every pull request and pushes it on:

- pushes to `main`: tags `main` and `sha-<commit>`,
- `v`-prefixed [SemVer] tags, e.g. `v0.1.0`: tags `v0.1.0`, `v0.1` and `latest`.

## Helm chart

`.github/workflows/chart.yaml` lints the chart on every push and pull request
and pushes it as an OCI artifact on `chart/`-prefixed tags, e.g. `chart/0.1.0`
(chart version `0.1.0`), via `make push-chart`.

Both workflows need the repository secrets `DOCKERHUB_USERNAME` and
`DOCKERHUB_TOKEN` (a Docker Hub access token with read/write access).

## Process

1. When releasing new images, bump the Helm chart's `appVersion` in
   `deployments/helm/kube-ovn-dra-driver/Chart.yaml` to the image version.
2. Tag and push. The image tag uses `v<version>`, matching the chart's
   `appVersion`, the chart tag `chart/<version>`; one commit may carry both:

   ```bash
   git tag -a v0.1.0 -m v0.1.0
   git tag -a chart/0.1.0 -m chart/0.1.0
   git push origin v0.1.0 chart/0.1.0
   ```
   The workflows push the image and the chart.
3. Draft and publish a [GitHub release][releases] with generated release notes.

[SemVer]: https://semver.org/
[releases]: https://github.com/soer3n/kube-ovn-dra-driver/releases
