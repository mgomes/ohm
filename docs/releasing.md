# Release Ohm

Git tags drive Ohm releases.

Push a `vX.Y.Z` tag. GitHub Actions runs GoReleaser. GoReleaser builds the
`ohm` CLI, creates archives, uploads checksums, and publishes a GitHub release.

## Version tag rule

Use semantic version tags with a leading `v`:

```text
v0.1.0
v0.1.1
v1.0.0
```

The release workflow only runs for tags that match:

```text
v*.*.*
```

## Check before tagging

Run the repo checks:

```sh
just check
```

Check the GoReleaser config:

```sh
goreleaser check
```

For a local build rehearsal, run:

```sh
goreleaser release --snapshot --clean
```

Snapshot builds do not publish a GitHub release.

## Publish

Create and push the tag:

```sh
git tag v0.1.0
git push origin v0.1.0
```

The workflow publishes the release with the repository `GITHUB_TOKEN`.

## Artifacts

The release builds `./cmd/ohm` as the `ohm` binary for:

```text
darwin amd64
darwin arm64
linux amd64
linux arm64
windows amd64
windows arm64
```

Archives use this naming shape:

```text
ohm_v0.1.0_linux_amd64.tar.gz
ohm_v0.1.0_darwin_arm64.tar.gz
ohm_v0.1.0_windows_amd64.zip
```

The checksum file uses SHA-256:

```text
ohm_v0.1.0_checksums.txt
```

Each archive includes:

- The `ohm` binary.
- `README.md`.
- The files under `docs/`.

## Version output

Released binaries print the tag:

```sh
ohm version
v0.1.0
```

GoReleaser sets that value at build time through linker flags.

Local builds that do not set linker flags print:

```text
dev
```

## Workflow

The GitHub Actions workflow:

- Checks out the full git history so GoReleaser can read tags.
- Sets up the latest available Go 1.25 patch release.
- Runs `goreleaser release --clean`.
- Grants `contents: write` so the release and assets can be created.

No release is published from a branch build. Branch builds should use
`goreleaser check` or a snapshot release.
