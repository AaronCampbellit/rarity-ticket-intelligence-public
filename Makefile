.DEFAULT_GOAL := verify

.PHONY: format test verify compose-config local-env

format:
	@if find backend -name '*.go' -type f -print -quit | grep -q .; then gofmt -w $$(find backend -name '*.go' -type f); fi
	@if [ -f frontend/package.json ]; then npm --prefix frontend run format; fi

test:
	@if find backend -name '*.go' -type f -print -quit | grep -q .; then go test ./backend/...; fi
	@if [ -f frontend/package.json ]; then npm --prefix frontend test -- --run; fi

compose-config:
	docker compose --env-file .env -f infrastructure/compose/compose.yaml config --quiet

verify: format test compose-config

local-env:
	./scripts/generate-local-env.sh
