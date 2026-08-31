.PHONY: build build-compat-askpass test test-shell-entrypoints test-platform cross-build test-docs test-licenses test-version test-race test-e2e test-screenshots test-public-screenshots test-public-snapshot public-snapshot test-installed-launcher test-docker test-container-docker test-container-podman test-workspace-docker test-system-config test-sources test-menu test-install test-install-full cover vet check-core check regression audit-public release-snapshot \
	toolchain-sync toolchain-build toolchain-build-lf toolchain-build-dtop toolchain-build-nvim toolchain-build-bat \
	toolchain-check toolchain-smoke toolchain-verify toolchain-ready remote-nvim remote-bundle test-remote-bundle install-user app-ready

TOOLCHAIN_DIR := $(CURDIR)/.toolchain
TOOLCHAIN_BIN := $(TOOLCHAIN_DIR)/bin
TOOLCHAIN_BUILD := $(TOOLCHAIN_DIR)/build
TOOLCHAIN_DIST := $(TOOLCHAIN_DIR)/dist
TOOLCHAIN_JOBS ?= $(shell getconf _NPROCESSORS_ONLN 2>/dev/null || printf '2')
LF_VERSION ?= r42
USER_BIN_DIR ?= $(HOME)/.local/bin

build:
	tools/build-sshf.sh bin/sshfleet

build-compat-askpass:
	CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o bin/sshf-askpass ./cmd/sshf-askpass

test:
	go test ./...

test-shell-entrypoints:
	go test -timeout=45s -v ./internal/tooling -run TestRegressionEntrypointFromSupportedShells -count=1

test-platform:
	go test ./internal/platform ./internal/config ./internal/inventory ./internal/localtarget ./internal/session ./cmd/sshf

cross-build:
	mkdir -p .tmp/cross-build .tmp/cross-build-tmp
	TMPDIR="$(CURDIR)/.tmp/cross-build-tmp" CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -buildvcs=false -trimpath -o .tmp/cross-build/sshfleet-darwin-amd64 ./cmd/sshf
	TMPDIR="$(CURDIR)/.tmp/cross-build-tmp" CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -buildvcs=false -trimpath -o .tmp/cross-build/sshfleet-windows-amd64.exe ./cmd/sshf

test-docs:
	go test -v ./internal/doccheck -count=1

test-licenses:
	tools/check-licenses.sh

test-version:
	tools/test-versioning.sh
	tools/test-release-workflow.sh
	tools/test-ci-podman.sh

test-race:
	go test -race ./...

test-e2e:
	go test -v ./cmd/sshf -run TestTUIEndToEnd -count=1

test-screenshots:
	rm -rf .artifacts/tui-screenshots
	mkdir -p .artifacts/tui-screenshots
	SSHF_SCREENSHOT_DIR="$(CURDIR)/.artifacts/tui-screenshots" go test -v ./cmd/sshf -run 'TestTUIEndToEnd(ScreenshotsAndActionMenuTraversal|GroupsCRUDAndMembership|LocalContainerMenuPreviewAndLogs|TrustedLocalConfigMenuAndPreview|GlobalHostSearchAcrossSources)' -count=1

test-public-screenshots:
	rm -rf .artifacts/public-screenshots
	mkdir -p .artifacts/public-screenshots
	SSHF_PUBLIC_SCREENSHOTS=1 SSHF_SCREENSHOT_DIR="$(CURDIR)/.artifacts/public-screenshots" \
		go test -v ./cmd/sshf -run 'TestTUIEndToEnd(ScreenshotsAndActionMenuTraversal|GroupsCRUDAndMembership)|TestPublicScreenshotSanitizer' -count=1
	magick compare -metric AE .artifacts/public-screenshots/wide-fleet.png docs/assets/screenshots/fleet-overview.png null:
	magick compare -metric AE .artifacts/public-screenshots/wide-menu-1.png docs/assets/screenshots/host-actions.png null:
	magick compare -metric AE .artifacts/public-screenshots/wide-terminal-tabs-completed.png docs/assets/screenshots/terminal-tabs.png null:
	magick compare -metric AE .artifacts/public-screenshots/groups-two-members.png docs/assets/screenshots/groups.png null:

test-installed-launcher: build
	tools/test-installed-launcher.sh

test-sources:
	go test -v ./internal/sourcebundle ./internal/inventory ./internal/config -count=1

test-menu:
	go test -v ./internal/ui -run 'TestHostActionMenu|TestGroup|TestHealthcheckOverlay|TestTerminalTab' -count=1

test-install: build
	tools/test-install.sh

test-install-full:
	tools/test-install-full.sh

SSHF_DOCKER_IMAGE ?= sshfleet-test-sshd:local
SSHF_PODMAN_IMAGE ?= sshfleet-test-sshd-podman:local
test-docker:
	docker build --tag "$(SSHF_DOCKER_IMAGE)" testdata/docker-sshd
	SSHF_DOCKER_E2E=1 SSHF_DOCKER_IMAGE="$(SSHF_DOCKER_IMAGE)" go test -v ./cmd/sshf -run TestDockerAliasHostKeyRepair -count=1

test-container-docker:
	docker build --tag "$(SSHF_DOCKER_IMAGE)" testdata/docker-sshd
	mkdir -p .artifacts/container-runtime-screenshots
	SSHF_CONTAINER_RUNTIME_E2E=1 SSHF_CONTAINER_RUNTIME=docker SSHF_CONTAINER_IMAGE="$(SSHF_DOCKER_IMAGE)" \
		SSHF_SCREENSHOT_DIR="$${SSHF_SCREENSHOT_DIR:-$(CURDIR)/.artifacts/container-runtime-screenshots}" \
		go test -v ./cmd/sshf -run TestContainerRuntimeTUIEndToEnd -count=1

test-container-podman:
	podman build --tag "$(SSHF_PODMAN_IMAGE)" testdata/docker-sshd
	mkdir -p .artifacts/container-runtime-screenshots
	SSHF_CONTAINER_RUNTIME_E2E=1 SSHF_CONTAINER_RUNTIME=podman SSHF_CONTAINER_IMAGE="$(SSHF_PODMAN_IMAGE)" \
		SSHF_SCREENSHOT_DIR="$${SSHF_SCREENSHOT_DIR:-$(CURDIR)/.artifacts/container-runtime-screenshots}" \
		go test -v ./cmd/sshf -run TestContainerRuntimeTUIEndToEnd -count=1

test-workspace-docker: remote-bundle
	docker build --tag "$(SSHF_DOCKER_IMAGE)" testdata/docker-sshd
	SSHF_DOCKER_E2E=1 SSHF_DOCKER_IMAGE="$(SSHF_DOCKER_IMAGE)" \
		SSHF_WORKSPACE_BUNDLE_TEST="$(CURDIR)/.toolchain/remote/sshfleet-tools-linux-amd64.tar.gz" \
		go test -v ./cmd/sshf -run TestDockerAliasHostKeyRepair -count=1

SSHF_SYSTEM_CONFIG_FIXTURE ?= $(CURDIR)/testdata/private/system-ssh-config.snapshot
test-system-config:
	SSHF_SYSTEM_CONFIG_FIXTURE="$(SSHF_SYSTEM_CONFIG_FIXTURE)" go test -v ./internal/sshconfig -run TestSystemSSHConfigSnapshot -count=1

cover:
	go test -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out

vet:
	go vet ./...

check-core: test test-platform cross-build test-docs test-licenses vet test-version test-install

check: check-core toolchain-check

audit-public:
	tools/public-audit.sh

test-public-snapshot:
	tools/test-public-snapshot.sh

PUBLIC_SNAPSHOT_DIR ?= $(CURDIR)/../sshfleet-console-public
public-snapshot:
	tools/create-public-snapshot.sh "$(PUBLIC_SNAPSHOT_DIR)"

release-snapshot: check-core
	rm -rf dist
	mkdir -p dist
	version_id="$$(./bin/sshfleet --version | awk '{print $$2}')"; \
	tools/build-release.sh "$$version_id" linux "$$(go env GOARCH)" dist
	cd dist && sha256sum sshfleet-console-*.tar.gz > checksums.txt

regression:
	tools/regression.sh

toolchain-sync:
	git submodule update --init --recursive

toolchain-build: toolchain-build-lf toolchain-build-dtop toolchain-build-nvim toolchain-build-bat

toolchain-build-lf: toolchain-sync
	mkdir -p "$(TOOLCHAIN_BIN)"
	cd tools/src/lf && CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w -X main.gVersion=$(LF_VERSION)" -o "$(TOOLCHAIN_BIN)/lf" .

toolchain-build-dtop: toolchain-sync
	mkdir -p "$(TOOLCHAIN_BIN)" "$(TOOLCHAIN_BUILD)/dtop"
	CARGO_TARGET_DIR="$(TOOLCHAIN_BUILD)/dtop" cargo build --manifest-path tools/src/dtop/Cargo.toml --release --locked --no-default-features
	install -m 0755 "$(TOOLCHAIN_BUILD)/dtop/release/dtop" "$(TOOLCHAIN_BIN)/dtop"

toolchain-build-nvim: toolchain-sync
	mkdir -p "$(TOOLCHAIN_BIN)" "$(TOOLCHAIN_DIST)/nvim"
	$(MAKE) -C tools/src/nvim -j"$(TOOLCHAIN_JOBS)" CMAKE_BUILD_TYPE=Release CMAKE_INSTALL_PREFIX="$(TOOLCHAIN_DIST)/nvim" install
	ln -sfn ../dist/nvim/bin/nvim "$(TOOLCHAIN_BIN)/nvim"

toolchain-build-bat: toolchain-sync
	mkdir -p "$(TOOLCHAIN_BIN)" "$(TOOLCHAIN_BUILD)/bat"
	CARGO_TARGET_DIR="$(TOOLCHAIN_BUILD)/bat" cargo build --manifest-path tools/src/bat/Cargo.toml --release --locked
	install -m 0755 "$(TOOLCHAIN_BUILD)/bat/release/bat" "$(TOOLCHAIN_BIN)/bat"
	ln -sfn bat "$(TOOLCHAIN_BIN)/batcat"

toolchain-check:
	tools/check.sh

toolchain-smoke: toolchain-check
	tools/check.sh --built

toolchain-verify: toolchain-smoke
	tools/verify.sh

toolchain-ready: toolchain-build
	$(MAKE) toolchain-verify

remote-nvim:
	tools/fetch-remote-nvim.sh

remote-bundle: toolchain-build-lf toolchain-build-dtop toolchain-build-bat remote-nvim
	tools/build-remote-bundle.sh

test-remote-bundle: remote-bundle
	tools/test-remote-bundle.sh

install-user: build
	mkdir -p "$(USER_BIN_DIR)"
	ln -sfn "$(CURDIR)/tools/launchers/sshfleet" "$(USER_BIN_DIR)/sshfleet"
	ln -sfn "$(CURDIR)/tools/launchers/sshf" "$(USER_BIN_DIR)/sshf"
	@if [ ! -e "$(USER_BIN_DIR)/sf" ] && [ ! -L "$(USER_BIN_DIR)/sf" ]; then \
		ln -s "$(CURDIR)/tools/launchers/sshfleet" "$(USER_BIN_DIR)/sf"; \
	else \
		printf '%s\n' 'SSH Fleet Console: sf already exists; optional alias was not changed'; \
	fi

app-ready: build toolchain-ready remote-bundle install-user
