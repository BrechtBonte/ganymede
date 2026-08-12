.PHONY: build up refresh launcher

LAUNCHER_APP := $(HOME)/Applications/Ganymede.app
LSREGISTER := /System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister

# build compiles the ganymede binary — the README's own Install step.
build:
	go build -o bin/ganymede ./cmd/ganymede

# up builds, then opens the harness for the current directory's repo.
up: build
	./bin/ganymede up

# refresh rebuilds and restarts an already-running Dashboard in place —
# what `up` alone cannot do, since it only ever reuses an existing
# Dashboard session rather than re-execing it. See scripts/refresh.sh.
refresh:
	./scripts/refresh.sh

# launcher materializes a minimal .app bundle in ~/Applications so Spotlight
# can find and launch Ganymede directly, without a terminal. Re-run after
# moving this checkout — the bundle bakes in its absolute path.
launcher: build
	mkdir -p "$(LAUNCHER_APP)/Contents/MacOS" "$(LAUNCHER_APP)/Contents/Resources"
	cp macos/launcher/Info.plist "$(LAUNCHER_APP)/Contents/Info.plist"
	sed "s|@@REPO_DIR@@|$(CURDIR)|g" macos/launcher/run.sh > "$(LAUNCHER_APP)/Contents/MacOS/Ganymede"
	chmod +x "$(LAUNCHER_APP)/Contents/MacOS/Ganymede"
	cp macos/launcher/Ganymede.icns "$(LAUNCHER_APP)/Contents/Resources/Ganymede.icns"
	$(LSREGISTER) -f "$(LAUNCHER_APP)"
	@echo "Installed $(LAUNCHER_APP) — search Spotlight for Ganymede."
