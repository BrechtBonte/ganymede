.PHONY: build up refresh

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
