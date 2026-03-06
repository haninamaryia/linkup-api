# Linkup API — Makefile
# Smoke tests: Dredd runs in Docker only, against API on same network (linkup-api:8181).

.PHONY: test-unit test-integration test-smoke pre-commit docker-build docker-run docker-up docker-down help

IMAGE_NAME ?= linkup-api
CONTAINER_NAME ?= linkup-api
NETWORK_NAME ?= linkup-net

help:
	@echo "test-unit       - Unit tests (services)"
	@echo "test-integration - Integration tests (handlers + in-memory DB)"
	@echo "test-smoke      - Dredd in Docker vs API container (API must be running: make docker-up)"
	@echo "pre-commit      - unit + integration + docker-up + test-smoke + docker-down"
	@echo "docker-build    - Build image"
	@echo "docker-run      - Run API in foreground"
	@echo "docker-up       - Start API in background on $(NETWORK_NAME)"
	@echo "docker-down     - Stop and remove API container"

test-unit:
	go test ./services/... -count=1 -short

test-integration:
	go test ./http/api/... -count=1

# Smoke: Dredd in Docker, same network as API. Requires API running (make docker-up first).
test-smoke:
	docker run --rm --network $(NETWORK_NAME) \
		-v $(CURDIR)/docs:/api-docs:ro -w /api-docs \
		-e NPM_CONFIG_LOGLEVEL=error \
		-e NPM_CONFIG_FUND=false \
		-e NPM_CONFIG_AUDIT=false \
		-e NODE_OPTIONS=--no-warnings \
		node:20-alpine \
		sh -c "npm install -g dredd --loglevel=error --no-fund --no-audit && dredd /api-docs/smoke-contracts.yaml http://$(CONTAINER_NAME):8181"

pre-commit: test-unit test-integration
	@echo "--- docker-up → test-smoke → docker-down ---"
	$(MAKE) docker-down 2>/dev/null || true
	$(MAKE) docker-build
	$(MAKE) docker-up
	@sleep 3
	@$(MAKE) test-smoke; ret=$$?; $(MAKE) docker-down; exit $$ret

docker-build:
	docker build -t $(IMAGE_NAME) .

docker-run: docker-build
	@mkdir -p data
	docker run --rm -p 8181:8181 \
		-e LINKUP_DATABASE_URL=/data/linkup.db \
		-v $$(pwd)/data:/data \
		$(IMAGE_NAME)

# Start API on shared network so Dredd can hit http://linkup-api:8181
docker-up: docker-build
	@mkdir -p data
	@docker network create $(NETWORK_NAME) 2>/dev/null || true
	@docker rm -f $(CONTAINER_NAME) 2>/dev/null || true
	docker run -d --name $(CONTAINER_NAME) --network $(NETWORK_NAME) -p 8181:8181 \
		-e LINKUP_DATABASE_URL=/data/linkup.db \
		-v $$(pwd)/data:/data \
		$(IMAGE_NAME)

docker-down:
	docker stop $(CONTAINER_NAME) 2>/dev/null || true
	docker rm $(CONTAINER_NAME) 2>/dev/null || true
