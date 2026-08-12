# Spotlight launcher

## Problem

Bringing up the harness today means opening a terminal and running `make up`
(or `./bin/ganymede up`) from this checkout. There's no way to launch
Ganymede the way any other macOS app is launched — from Spotlight.

## Why not existing options

Spotlight only indexes actual `.app` bundles (in `/Applications`,
`~/Applications`, and a few other well-known locations) — not scripts or bare
executables, however runnable. So the choice is really about how the `.app`
gets built:

- **Hand-rolled bundle (chosen)** — a bare `Info.plist` plus a small shell
  script as the bundle's executable, checked into the repo as a template and
  materialized by a Makefile target. No new dependency, fully versioned,
  consistent with this repo's existing Makefile/`scripts/` style.
- **Automator app** — rejected. Built through a GUI step with nothing to
  check into git, and it drifts out of sync with the repo silently.
- **Third-party wrapper (e.g. Platypus)** — rejected. Adds a new Prerequisite
  for a job the hand-rolled bundle already does in a dozen lines.

## Design

### Layout

```
macos/launcher/
  Info.plist       # static bundle metadata
  run.sh            # template; @@REPO_DIR@@ substituted at install time
  Ganymede.icns     # app icon (moon artwork), copied in during implementation
                     # from ~/Downloads/Moon_rzwvYKRUV2_icns-5907758bd0.icns
```

### `Info.plist`

Static — no substitution needed:

| Key | Value |
|---|---|
| `CFBundleName` / `CFBundleDisplayName` | `Ganymede` |
| `CFBundleIdentifier` | `com.brechtbonte.ganymede` |
| `CFBundleExecutable` | `Ganymede` |
| `CFBundleIconFile` | `Ganymede` (resolves to `Ganymede.icns`) |
| `CFBundlePackageType` | `APPL` |
| `LSUIElement` | `<true/>` (the plist boolean tag, not the string `"true"`)
  — the bundle's own process has no window and no reason to sit in the Dock
  or Cmd+Tab; Ghostty's window is the real UI. |

### `run.sh`

```sh
#!/usr/bin/env bash
set -euo pipefail
export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"
exec "@@REPO_DIR@@/bin/ganymede" up "@@REPO_DIR@@"
```

`@@REPO_DIR@@` is substituted with this checkout's absolute path when the
bundle is materialized, so the installed app always calls this checkout's
binary, opened at this checkout as the first Session's main root — matching
today's `make up`.

The explicit `PATH` export matters here in a way it wouldn't for a plain
shell script: a process launched by LaunchServices (double-click, Spotlight,
`open`) inherits the login session's default environment, not an interactive
shell's rc-augmented one, so Homebrew's `/opt/homebrew/bin` is not guaranteed
to be on it. `up` shells out to `tmux` and looks up `terminal-notifier` by
bare name — both Homebrew-installed — so without this export a Spotlight
launch could fail even though `make up` works fine from a terminal.

Launching again while the harness is already up costs nothing extra:
`ganymede up` is already idempotent (`harness.Attached()` → `Activate()`), so
a repeat Spotlight launch just refocuses the existing Ghostty window, which is
exactly what you want from an app icon.

### Makefile

New `.PHONY` target, `launcher`, depending on `build` — added to the existing
`.PHONY: build up refresh` line as `.PHONY: build up refresh launcher`:

```makefile
LAUNCHER_APP := $(HOME)/Applications/Ganymede.app
LSREGISTER := /System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister

# launcher materializes a minimal .app bundle in ~/Applications so Spotlight
# can find and launch Ganymede directly. Re-run after moving this checkout —
# the bundle bakes in its absolute path.
launcher: build
	mkdir -p "$(LAUNCHER_APP)/Contents/MacOS" "$(LAUNCHER_APP)/Contents/Resources"
	cp macos/launcher/Info.plist "$(LAUNCHER_APP)/Contents/Info.plist"
	sed "s|@@REPO_DIR@@|$(CURDIR)|g" macos/launcher/run.sh > "$(LAUNCHER_APP)/Contents/MacOS/Ganymede"
	chmod +x "$(LAUNCHER_APP)/Contents/MacOS/Ganymede"
	cp macos/launcher/Ganymede.icns "$(LAUNCHER_APP)/Contents/Resources/Ganymede.icns"
	$(LSREGISTER) -f "$(LAUNCHER_APP)"
	@echo "Installed $(LAUNCHER_APP) — search Spotlight for Ganymede."
```

`lsregister -f` registers the bundle with Launch Services immediately, so it
shows up in Spotlight right away instead of waiting on background indexing.

### Install location

`~/Applications` — user-only, no elevated permissions, Spotlight indexes it
by default.

### Signing

Not needed. The bundle is built and run locally, never downloaded, so
Gatekeeper quarantine doesn't apply.

## Testing

- `make launcher` succeeds and produces
  `~/Applications/Ganymede.app/Contents/{Info.plist,MacOS/Ganymede,Resources/Ganymede.icns}`.
- `open ~/Applications/Ganymede.app` (simulating a Spotlight launch) opens
  the Ghostty window with the Dashboard, same as `make up`.
- Running it again while already up refocuses the existing window rather than
  opening a second one.
- Spotlight search for "Ganymede" surfaces the app with the moon icon.

## Out of scope

- Code signing / notarization.
- A UI for picking which directory to open at launch time.
- Auto-regenerating the bundle on every `make build` — `launcher` is a
  separate, explicit step.
- Distributing the bundle to another machine or user — it is tied to this
  checkout's absolute path by design.
