# Release Process

The DRA example driver is released on an as-needed basis. Published release
artifacts include:

- The dra-example-driver Helm chart
- Container images

The Helm chart may be released independently from the container images. When
releasing new container images, a new release of the Helm chart should usually
be cut at the same time.

The process for both is as follows:

1. An issue proposing a new release is opened.
   The issue should describe if one or both of the container image and Helm
   chart should be released.
     - Example issue: https://github.com/kubernetes-sigs/dra-example-driver/issues/119
1. At least two of the [OWNERS](OWNERS) must agree to move forward with this
   release.
1. When releasing new container images, the Helm chart's `appVersion` in
   Chart.yaml is updated to reflect the version of the images to be cut.
    - Example PR: https://github.com/kubernetes-sigs/dra-example-driver/pull/120
1. Depending on what is being released, an OWNER runs `git tag -a $VERSION -m $VERSION`
   and pushes the tag with `git push <remote> $VERSION`
    - When releasing the container images, `$VERSION` is a `v`-prefixed
      [SemVer], e.g. `v0.1.0`
    - When releasing the Helm chart, `$VERSION` is a `chart/`-prefixed [SemVer],
      e.g. `chart/0.1.0`
    - The same commit may be tagged with a tag of each form to release both the
      Helm chart and container images at the same time.
    - `<remote>` refers to the name of the Git remote for the repo at
      [https://github.com/kubernetes-sigs/dra-example-driver], likely either
      `upstream` or `origin`
    - Each pushed tag triggers builds of artifacts pushed to the staging
      repository for the [dra-example-driver container image][container-staging]
      and [Helm chart][chart-staging].
1. When releasing new container images, an OWNER drafts (but does not yet
   publish) a [GitHub release][releases] with GitHub's generated release notes.
1. A PR updating the [registry.k8s.io image list][image list] is opened with the
   SHA digests and tags of the new artifacts from the staging repo.
    - Example PR: https://github.com/kubernetes/k8s.io/pull/7871
    - After the PR merges, a run of the [promotion job] will kick off.
1. The artifacts are verified to be available from registry.k8s.io:
    - e.g. the v0.2.0 image and 0.2.0 chart can be verified with the following
      commands:
        - `docker manifest inspect registry.k8s.io/dra-example-driver/dra-example-driver:v0.2.0`
        - `docker manifest inspect registry.k8s.io/dra-example-driver/charts/dra-example-driver:0.2.0`
1. The drafted [GitHub release][releases] is published.
1. The release issue is closed.
1. An announcement is made to [#wg-device-management] on Slack.
  - e.g. [https://kubernetes.slack.com/archives/C0409NGC1TK/p1756395401313149]

[SemVer]: https://semver.org/
[staging repo]: https://console.cloud.google.com/artifacts/docker/k8s-staging-images/us-central1/dra-example-driver?inv=1&invt=Abs5-A&project=k8s-staging-images
[chart-staging]: https://console.cloud.google.com/artifacts/docker/k8s-staging-images/us-central1/dra-example-driver/charts%2Fdra-example-driver?inv=1&invt=Abs5-A&project=k8s-staging-images
[container-staging]: https://console.cloud.google.com/artifacts/docker/k8s-staging-images/us-central1/dra-example-driver/dra-example-driver?inv=1&invt=Abs5-A&project=k8s-staging-images
[image list]: https://github.com/kubernetes/k8s.io/blob/main/registry.k8s.io/images/k8s-staging-dra-example-driver/images.yaml
[promotion job]: https://testgrid.k8s.io/sig-k8s-infra-k8sio#post-k8sio-image-promo
[releases]: https://github.com/kubernetes-sigs/dra-example-driver/releases
[#wg-device-management]: https://kubernetes.slack.com/archives/C0409NGC1TK
