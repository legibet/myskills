# Release

Releases are semver tags `vX.Y.Z`. `.github/workflows/release.yml` builds every pushed tag, the CLI with goreleaser (`.goreleaser.yaml`) and the desktop app with mygo (`app/mygo.json`), and publishes the GitHub release once all assets are built.

## Steps

1. Pick the version from the commits since the last tag: while 0.x, a minor bump for new features or changed behavior, a patch bump for fixes only.
2. Write the release notes to a temporary file: a short Markdown list of what changed for users, in their terms, not the commit log.
3. Run `make release VERSION=x.y.z NOTES=<file>`. It refuses unless HEAD is a clean, pushed `main` whose Test run passed. It sets the version in `app/mygo.json`, commits it, tags it with the notes as the message, and pushes.
4. The release is done when the Release run of the tag passed and `gh release view vX.Y.Z` lists its assets.

## Desktop app assets

The app's assets are published under names without the version, so `releases/latest/download/<name>` links in the README keep working:

- macOS: `MySkills.dmg`, a universal build signed ad hoc. It is not notarized, so the README says how to clear the quarantine attribute.
- Linux: `MySkills-linux-<arch>.deb`, `MySkills-linux-<arch>.tar.gz`, and `MySkills-install.sh`, which installs the archive for the user.
- Windows: `MySkills-setup-<arch>.exe`, an unsigned installer that installs the app for the user.

The icon is `app/icon`: the Icon Composer `AppIcon.icon` on macOS, `icon.png` elsewhere.
