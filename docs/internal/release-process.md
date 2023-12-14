# Release process

## Main package

1. Create a new tag with the format `v<MAJOR>.<MINOR>.<PATCH>` depending on the
   changes since the last release.

2. Create a new GitHub release by picking the new tag and also click the
   "Generate release notes" button to fill the release information with
   information about the changes since the previous release.

## Helm charts

1. Bump the chart version in `charts/k6-loadtester/Chart.yaml` depending on the
   changes since the last release of the Helm charts and push that updated chart
   file to the `main` branch.

The tag and the actual release will then be handled by
[helm/chart-releaser-action](https://github.com/helm/chart-releaser-action).
