VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
# Listed explicitly so Go doesn't scan web/node_modules.
GO_PKGS := ./cmd/... ./internal/... ./dev/... ./web
BUNDLE := dist/iglo-$(VERSION)-linux-amd64

.PHONY: web build bundle test dev dev-code dev-api dev-web shellcheck deploy

web:
	cd web && npm ci && npm run build

build: web
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/iglo ./cmd/iglo
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/iglo-agent ./cmd/iglo-agent
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/stacks/arr/iglo-arr ./cmd/iglo-arr
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/stacks/jellyfin/iglo-jellyfin ./cmd/iglo-jellyfin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/stacks/music/iglo-music ./cmd/iglo-music
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/stacks/inbox/iglo-inbox ./cmd/iglo-inbox

bundle: build
	echo "$(VERSION)" >$(BUNDLE)/VERSION
	install -m 0755 deploy/install.sh $(BUNDLE)/install.sh
	install -m 0644 deploy/lib.sh deploy/storage.sh deploy/secrets.sh deploy/scoped-stack.sh deploy/iglo.service deploy/iglo-agent.service deploy/iglo-mdns.service 'deploy/iglo-secrets@.service' deploy/iglo-power.service $(BUNDLE)/
	install -m 0755 deploy/power.sh $(BUNDLE)/power.sh
	install -m 0755 deploy/iglo-mdns $(BUNDLE)/iglo-mdns
	install -m 0755 deploy/stacks/arr/install.sh $(BUNDLE)/stacks/arr/install.sh
	install -m 0644 deploy/stacks/arr/compose.yaml deploy/stacks/arr/recyclarr.yml $(BUNDLE)/stacks/arr/
	install -m 0755 deploy/stacks/jellyfin/install.sh $(BUNDLE)/stacks/jellyfin/install.sh
	mkdir -p $(BUNDLE)/stacks/photos
	install -m 0755 deploy/stacks/photos/install.sh deploy/stacks/photos/recovery.sh $(BUNDLE)/stacks/photos/
	install -m 0644 deploy/stacks/photos/compose.yaml deploy/stacks/photos/compose.override.yaml deploy/stacks/photos/compose.gpu.yaml deploy/stacks/photos/backup.sh $(BUNDLE)/stacks/photos/
	install -m 0700 deploy/iglo-photos-key $(BUNDLE)/iglo-photos-key
	mkdir -p $(BUNDLE)/stacks/music $(BUNDLE)/stacks/inbox
	install -m 0755 deploy/stacks/music/install.sh $(BUNDLE)/stacks/music/
	install -m 0644 deploy/stacks/music/iglo-music.service $(BUNDLE)/stacks/music/
	install -m 0755 deploy/stacks/inbox/install.sh $(BUNDLE)/stacks/inbox/
	install -m 0644 deploy/stacks/inbox/iglo-inbox.service $(BUNDLE)/stacks/inbox/
	mkdir -p $(BUNDLE)/stacks/gaming
	install -m 0755 deploy/stacks/gaming/install.sh deploy/stacks/gaming/diagnose.sh $(BUNDLE)/stacks/gaming/
	install -m 0644 deploy/stacks/gaming/compose.yaml deploy/stacks/gaming/config.toml $(BUNDLE)/stacks/gaming/
	mkdir -p $(BUNDLE)/stacks/headscale
	install -m 0755 deploy/stacks/headscale/install.sh deploy/stacks/headscale/guest.sh $(BUNDLE)/stacks/headscale/
	install -m 0644 deploy/stacks/headscale/validate.py deploy/stacks/headscale/config.yaml.in deploy/stacks/headscale/Caddyfile.in deploy/stacks/headscale/headscale.service deploy/stacks/headscale/caddy.service deploy/stacks/headscale/nftables.conf $(BUNDLE)/stacks/headscale/
	# COPYFILE_DISABLE stops macOS tar from adding ._ files, which the agent refuses.
	COPYFILE_DISABLE=1 tar -czf $(BUNDLE).tar.gz -C dist $(notdir $(BUNDLE))

# deploy checks, builds, signs and installs the tagged commit on your host:
# make deploy HOST=root@pve. See docs/development.md.
deploy:
	deploy/deploy.sh "$(HOST)"

# Every shell script, also the ones without .sh, checked with the sourced
# files they name. Pinned, so CI and a laptop find the same things.
SHELL_SCRIPTS := $(shell git ls-files '*.sh') deploy/iglo-mdns deploy/iglo-photos-key dev/bin/tailscale
shellcheck:
	docker run --rm -v "$(CURDIR):/mnt" -w /mnt koalaman/shellcheck:v0.10.0 -x -S warning $(SHELL_SCRIPTS)

test:
	go vet $(GO_PKGS)
	go test $(GO_PKGS)
	cd web && npm test && npm run lint && npm run typecheck

# Everything on this computer, with a fake Proxmox and host agent. Open http://localhost:5173.
dev:
	./dev/run.sh

# The login code for the dev admin.
dev-code:
	@go run ./dev/mock code

# Against a real Proxmox. Needs PROXMOX_URL, PROXMOX_TOKEN_ID and PROXMOX_TOKEN_SECRET, and an admin
# created with: IGLO_DATA_DIR=.data go run ./cmd/iglo admin
dev-api:
	IGLO_DEV=1 IGLO_HTTP_ADDR=127.0.0.1:8080 IGLO_DATA_DIR=.data go run ./cmd/iglo serve

dev-web:
	cd web && npm run dev
