# Releasing Rex

Rex releases are created from annotated version tags in the form `vX.Y.Z`.
Pushing a matching tag runs the release workflow, which cross-compiles the
project's five command-line programs, packages the release files, creates a
SHA-256 manifest, and publishes a GitHub release with generated notes.

## Release procedure

1. Merge the intended changes to `main` and make sure CI is green.
2. Choose the semantic version and create an annotated tag from the reviewed
   commit, for example `git tag -a v0.1.0 -m "REX v0.1.0"`.
3. Push the tag with `git push origin v0.1.0`.
4. Review the GitHub release: confirm every archive and `checksums.txt` is
   attached, then edit the generated notes if the release needs operator
   instructions or an upgrade warning.

The workflow never publishes from a branch or an unversioned commit. Deleting
and recreating a release tag is discouraged; prepare a corrected patch release
instead.

## Download verification

Download the matching archive and `checksums.txt` from the GitHub release.
On Linux, verify it with:

```bash
sha256sum --ignore-missing --check checksums.txt
```

On macOS, use:

```bash
shasum -a 256 --check --ignore-missing checksums.txt
```

The checksums detect accidental corruption. They are not signatures, so obtain
the manifest from the GitHub release and rely on the repository's release
permissions for its provenance.

## Archive contents

Each archive contains `rexc`, `rexd`, `redis_setup`, `rex_stressor`, and
`rule_gen`, plus `README.md` and `LICENSE`. Windows executables end in `.exe`.
The release filename identifies the tag, operating system, and architecture:
`rex_<tag>_<os>_<arch>.tar.gz` (or `.zip` on Windows).
