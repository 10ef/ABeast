# Releases

The workflows need no custom secrets for the current unsigned/ad hoc builds. GitHub supplies `GITHUB_TOKEN`; only the draft-release job requests repository write access. Third-party actions are pinned to commit SHAs.

## Continuous integration

[CI](../.github/workflows/ci.yml) runs on pushes to `main`, pull requests, manual dispatch, and calls from the release workflow. It downloads and verifies modules, then runs `go test ./...` with cgo disabled on macOS and Windows. It also checks the packaging script and override script with Python and Node.js tests. The Go version comes from `go.mod`.

[Release](../.github/workflows/release.yml) runs when a `v*` tag is pushed. It requires the tag to equal `v` plus the stable `x.y.z` version in `mygo.json`, runs CI, builds all three packages, verifies the asset set, writes SHA-256 checksums, and creates a **draft** release. A failed test or build prevents creation of the draft. Reruns can replace assets on an existing draft; they refuse to modify an already published release.

## Prepare a version

1. Set `version` in `mygo.json`, for example `0.1.0`, and review the README against the current app.
2. Run `go test ./...`, then test the packaged app on macOS and Windows. Check tray states, exit probes, notifications, protection on/off, and restoration on exit using disposable agent processes and a test proxy configuration.
3. Commit the intended source changes and push `main`. Wait for CI to pass.
4. Create and push the matching tag:

```sh
git tag -a v0.1.0 -m "ABeast v0.1.0"
git push origin v0.1.0
```

5. Wait for Release to finish. Open the draft, replace the maintainer paragraph with version-specific notes, download and smoke-test the assets, then publish it manually. Do not move an already published version tag; ship a new version instead.

## Outputs

| Asset | Contents |
| --- | --- |
| `ABeast-<version>-darwin-universal.dmg` | Universal macOS app, Apple silicon and Intel |
| `ABeast-<version>-windows-amd64.zip` | x64 executable and companion resources |
| `ABeast-<version>-windows-arm64.zip` | ARM64 executable and companion resources |
| `SHA256SUMS.txt` | SHA-256 digest of each package |

Windows builds are cross-compiled on Linux and zipped with their staged resources; an optional local NSIS installer is not a release asset. CI runs Windows tests on its native runner, but it does not exercise the desktop UI or run the ARM64 Windows app. Manual platform testing remains necessary.

## Reproduce packaging locally

Use macOS for the universal app and DMG, and Python 3.11 or later for the packaging script. Build into a fresh output directory so a package cannot pick up files from an older build:

```sh
go tool mygo build -platform darwin/universal,windows/amd64,windows/arm64 -o build/release
python3 scripts/release.py check-tag v0.1.0
python3 scripts/release.py package darwin-universal --build build/release
python3 scripts/release.py package windows-amd64 --build build/release
python3 scripts/release.py package windows-arm64 --build build/release
python3 scripts/release.py checksums
```

Use a fresh `dist/` directory for each version. The checksum step rejects missing or unexpected assets. On macOS, verify downloaded files with `shasum -a 256 -c SHA256SUMS.txt`; on Windows, compare `Get-FileHash -Algorithm SHA256 <file>` with the corresponding entry.

The app resources include the project license and third-party notices. Keep `resources/LICENSE` identical to `LICENSE` and refresh `resources/THIRD_PARTY_NOTICES.txt` when dependencies change.

## Signing

Current builds use macOS ad hoc signing and no notarization; Windows has no Authenticode certificate. Do not describe these artifacts as notarized or verified by a commercial signing identity. For signed distribution, configure mygo's signing/notarization support, store credentials in GitHub secrets, and validate a downloaded artifact before updating the installation documentation.
