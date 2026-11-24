# Releasing

Releases are cut by [release-please](https://github.com/googleapis/release-please-action) from the Conventional Commit messages on
`main`. Every build of `main` publishes development artifacts; merging the release pull request publishes a release. Kubelook is
deployed as a container (the image or the chart); standalone binaries are not published. Nothing is published from pull requests,
the merge queue, other branches or tags.

## How a release happens

1. Every push to `main` runs the whole `ci` workflow. When every check passed, the `Release` job runs release-please. It opens or
   updates one **release pull request** that bumps `VERSION` and adds the changes since the last release to `CHANGELOG.md`.
2. You review that pull request like any other and merge it when you want to release. It goes through the same checks.
3. The build of that merge runs the checks again, then release-please creates the tag `v<version>` on the tested commit and the GitHub
   release with the changelog as its notes. The publishing jobs then push the version tags of the image and the chart.

The `Release` job runs only after every check passed, so a release is never cut from a commit whose checks failed.

## What a `main` build publishes

| Artifact | Every build of `main` | A release build (additionally) |
| --- | --- | --- |
| Image `ghcr.io/bdnix/kubelook` (amd64 and arm64) | tags `main` and `sha-<commit>`, a cosign signature, provenance, an SBOM per architecture | tag `<version>`, and `<major>.<minor>` for a final version |
| Chart `oci://ghcr.io/bdnix/charts/kubelook` | version `<VERSION>-main.<short-sha>` | version `<version>`, and the archive attached to the GitHub release |

The image that is published is the one the `build` job made and the tests used: the publishing job loads those archives and pushes
them; nothing is rebuilt. The chart is published with `image.digest` set to that image's digest. Both jobs then check what they
published: the signature and the attestations are verified from the registry (`cosign verify`, `gh attestation verify`), the image is
pulled and run, and the chart is pulled back and rendered.

## Versions

`VERSION` holds the current version and `.release-please-manifest.json` the last released one; release-please updates both.
`release-please-config.json` makes every release a release candidate (`0.1.0-rc.2`, `0.1.0-rc.3`, …): versioning `prerelease`,
prerelease type `rc`, GitHub releases marked as pre-releases. A release candidate's notes and the chart's `README.md` start with "Release
candidate: the test against a real organisation's Vault is still open."

To cut the first final version, `0.1.0`, after the human gate in `docs/security-review.md` has passed: set `"prerelease": false` and
`"versioning": "default"` in `release-please-config.json`, and commit with a `Release-As: 0.1.0` footer. The next release pull request
proposes `0.1.0`.

This configuration has not produced a release yet. Check the first release pull request: that it proposes `0.1.0-rc.2` and that its
changelog starts after `v0.1.0-rc.1`.

## The token release-please needs

Pull requests and releases created with the workflow's own `GITHUB_TOKEN` do not trigger workflows, so the release pull request would
never get its checks and could not be merged. The `Release` job therefore uses the secret `GH_PAT`: a fine-grained personal access
token limited to this repository, with read and write access to contents and pull requests. It is an environment secret of
`production`, so only the jobs that declare that environment can read it, and only on `main`. Without the secret the job falls back
to `GITHUB_TOKEN`; the release pull request then needs its checks started by hand (close and reopen it).

If the job fails while labelling its pull request, the token also needs read and write access to issues: release-please labels its
pull requests through the issues API.

## Verifying a release

```bash
cosign verify ghcr.io/bdnix/kubelook:<version> \
  --certificate-identity 'https://github.com/bdnix/kubelook/.github/workflows/ci.yml@refs/heads/main' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com
gh attestation verify oci://ghcr.io/bdnix/kubelook:<version> --repo bdnix/kubelook \
  --signer-workflow bdnix/kubelook/.github/workflows/ci.yml
```

The publishing job runs both on every build of `main`.

## Rollback

A released version is never published again. Fix forward: merge the fix, and merge the next release pull request.

## Settings of the repository

- **The `production` environment.** The release and publishing jobs (`Release`, `publish-image`, `publish-chart`) and the `netshoot`
  workflow declare `environment: production`. Restrict it to the `main` branch (*Settings → Environments → production → Deployment
  branches and tags*), so that a workflow edited on another branch cannot publish. Optionally add required reviewers: every
  publishing run then waits for an approval.
- **Pushes to `main`** publish, so protect `main` and require pull requests (see `docs/ci.md`).
