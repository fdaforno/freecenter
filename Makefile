# Personal settings (registry, deployment) live in Makefile.local, which is not
# part of the repository: see the end of this file.
-include Makefile.local

IMAGE ?= hostwarden
TAG   ?= latest

.PHONY: build test vet run clean docker ui ui-lint ui-dev

UI_DIR  := web/ui
UI_DIST := web/dist/index.html

ui: ## build the web console into web/dist (embedded in the server binary)
	cd $(UI_DIR) && { [ -d node_modules ] || npm ci; } && npm run build

ui-lint: ## lint the web console
	cd $(UI_DIR) && npx ng lint

ui-dev: ## web console with hot reload on :4200, /api forwarded to :8080
	cd $(UI_DIR) && npm start

# go:embed needs web/dist: build it when it is missing
$(UI_DIST):
	$(MAKE) ui

build: ui ## build the server
	go build ./...

vet: $(UI_DIST)
	go vet ./...

test: $(UI_DIST)
	go test ./...

run: ui ## run the server on :8080 with its data in ./data
	go run ./cmd/hostwarden

clean:
	rm -rf hostwarden web/dist

docker: ## build the container image
	docker build -t $(IMAGE):$(TAG) .

# Makefile.local can set IMAGE and TAG and add targets of your own, e.g. to
# push a multi-architecture image to your registry and redeploy:
#
#   IMAGE := registry.example.com/hostwarden
#
#   .PHONY: docker-push deploy
#   docker-push:
#   	docker buildx build --platform linux/amd64,linux/arm64 -t $(IMAGE):$(TAG) --push .
#   deploy: docker-push
#   	./deploy.sh
