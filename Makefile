VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
# The GitHub repo that the manager checks for new releases.
REPO ?= $(shell git remote get-url origin 2>/dev/null | sed -E 's,(git@github.com:|https://github.com/),,; s,\.git$$,,')
LDFLAGS := -s -w -X main.version=$(VERSION) -X iglo/internal/selfupdate.DefaultRepo=$(REPO)
# Listed explicitly so Go doesn't scan web/node_modules.
GO_PKGS := ./cmd/... ./internal/... ./dev/... ./web
BUNDLE := dist/iglo-$(VERSION)-linux-amd64

.PHONY: web build bundle test dev dev-code dev-api dev-web

web:
	cd web && npm ci && npm run build

build: web
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/iglo ./cmd/iglo
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/iglo-agent ./cmd/iglo-agent
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/stacks/arr/iglo-arr ./cmd/iglo-arr
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)" -o $(BUNDLE)/stacks/jellyfin/iglo-jellyfin ./cmd/iglo-jellyfin

bundle: build
	echo "$(VERSION)" >$(BUNDLE)/VERSION
	install -m 0755 deploy/install.sh $(BUNDLE)/install.sh
	install -m 0644 deploy/lib.sh deploy/storage.sh deploy/iglo.service deploy/iglo-agent.service deploy/iglo-mdns.service $(BUNDLE)/
	install -m 0755 deploy/iglo-mdns $(BUNDLE)/iglo-mdns
	install -m 0755 deploy/stacks/arr/install.sh $(BUNDLE)/stacks/arr/install.sh
	install -m 0644 deploy/stacks/arr/compose.yaml deploy/stacks/arr/recyclarr.yml $(BUNDLE)/stacks/arr/
	install -m 0755 deploy/stacks/jellyfin/install.sh $(BUNDLE)/stacks/jellyfin/install.sh
	mkdir -p $(BUNDLE)/stacks/minecraft
	install -m 0755 deploy/stacks/minecraft/install.sh $(BUNDLE)/stacks/minecraft/install.sh
	install -m 0644 deploy/stacks/minecraft/compose.yaml $(BUNDLE)/stacks/minecraft/
	# COPYFILE_DISABLE stops macOS tar from adding ._ files, which the agent refuses.
	COPYFILE_DISABLE=1 tar -czf $(BUNDLE).tar.gz -C dist $(notdir $(BUNDLE))

test:
	go vet $(GO_PKGS)
	go test $(GO_PKGS)
	cd web && npm run lint && npm run typecheck

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
