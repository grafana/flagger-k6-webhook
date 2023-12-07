# Release process

## Main package

1. Create a new tag with the format `v<MAJOR>.<MINOR>.<PATCH>` depending on the
   changes since the last release.

2. Create a new GitHub release by picking the new tag and also click the
   "Generate release notes" button to fill the release information with
   information about the changes since the previous release.

## Helm charts

1. Create a new tag with the format `k6-loadtester-<MAJOR>.<MINOR>.<PATCH>`
   depending on the changes since the last release of the Helm charts.

2. Create a new GitHub release by picking the new tag and manually add notes
   about the changes you want to cover.
