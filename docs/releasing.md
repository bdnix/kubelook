# Releasing

Every build of `main` publishes development artifacts. A merge that changes the `VERSION` file also publishes a release: signed
multi-architecture images, the Helm chart as an OCI artifact, a tag and a GitHub release. Kubelook is deployed as a container (the
image or the chart); standalone binaries are not published. Nothing is published from pull requests, other branches or tags: a
release is a reviewed pull request, and the tag is an output of the build, never its trigger.

## What a `main` build publishes

| Artifact | Every build of `main` | A release build (additionally) |
| --- | --- | --- |
| Image `ghcr.io/bdnix/kubelook` | tags `main` and `sha-<commit>`, SBOM, provenance, cosign signature | tag `<version>`, and `<major>.<minor>` unless the version is a release candidate; a Trivy scan of the pushed digest that fails on fixable HIGH or CRITICAL vulnerabilities |
| Chart `oci://ghcr.io/bdnix/charts/kubelook` | version `<version>-main.<short-sha>` | version `<version>` |
| Tag and GitHub release | none | tag `v<version>` on the tested commit, a release with generated notes and the chart archive, marked as a pre-release for a release candidate |

The chart is published with `image.digest` set to the digest of the image of the same build, so a chart always installs the image it was
tested with.

## The `VERSION` file

`VERSION` holds one line: a semantic version without `v`, optionally with a release-candidate suffix (`0.1.0-rc.1`).
`hack/version.sh` prints it after checking its form, and `hack/version.sh --prerelease` prints `true` when the suffix is present.
The `backend-tests` job runs the tests of the script (`make test-release-scripts`), so a malformed `VERSION` fails the pull request.

A release candidate is published like a release with three differences: the GitHub release is created with `--prerelease`, the image
does not get the moving `<major>.<minor>` tag, and the chart `README.md` and the release notes start with: "Release candidate: the
external security review and the test against a real organisation's Vault are still open."

## How a build decides that it is a release

A `main` build is a release build when `v<VERSION>` does not exist as a tag. The job `publish-image` checks the tags of the repository
(`git ls-remote --tags`), not the diff of the commit. A release whose build failed is therefore published by the next green build of
`main`, and a revert never publishes the same version twice. `create-release` runs only on release builds and creates the tag on the
commit that was built (`gh release create --target "$GITHUB_SHA"`); no job pushes a tag.

## Cutting a release

1. Check the preconditions: `main` is green including `integration-tests` and `chart-install-tests`; `docs/spike-findings.md` is up to
   date; `hack/pin-netshoot.sh` was run after the last netshoot update; and, for a version without the `-rc` suffix, both human gates
   (a real organisation's Vault, and the external security review and penetration test) are recorded as passed in
   `docs/security-review.md`.
2. Open a pull request that changes `VERSION` and nothing else is required.
3. Merge it once every required check is green. The build of the merge publishes the release.

The plan publishes only release candidates (`0.1.0-rc.1` first). Cutting `0.1.0` is the repository owner's decision, after the gates.

## Verifying a release

```bash
cosign verify ghcr.io/bdnix/kubelook:<version> \
  --certificate-identity-regexp 'https://github.com/bdnix/kubelook/.*' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify oci://ghcr.io/bdnix/kubelook:<version> --repo bdnix/kubelook
```

## Rollback

A released version is never re-published. Fix forward: a pull request that bumps `VERSION` again.

## Settings of the repository

Pushing to `main` publishes, so restrict who can push to it (see `docs/ci.md`). The workflow runs the publishing jobs only on `push` to
`main`; fork and Dependabot pull requests never get a token that can push, sign or release. `main` builds are never cancelled by a newer
push, so two quick merges each publish.
