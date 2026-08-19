.PHONY: build up down logs test smoke-test dynamic-test rotate-demo fmt vet

build:
	docker compose build

up:
	docker compose up --build -d

down:
	docker compose down

logs:
	docker compose logs -f

test:
	go test ./...

smoke-test:
	./scripts/smoke-test.sh

dynamic-test:
	./scripts/test-dynamic-update.sh

rotate-demo:
	./scripts/rotate-demo.sh

fmt:
	gofmt -w $$(find cmd internal -name '*.go' -type f)

vet:
	go vet ./...
