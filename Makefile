REGION      ?= europe-west2
IMAGE_NAME  ?= bulwarkai
IMAGE_TAG   ?= latest
SERVICE     ?= bulwarkai

-include .env
export

REPO ?= europe-west2-docker.pkg.dev/$(GOOGLE_CLOUD_PROJECT)/bulwarkai/$(IMAGE_NAME)

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS = -X main.version=$(VERSION)

.PHONY: dev test build push deploy run-local clean sast scan docs

dev:
	go run -ldflags "$(LDFLAGS)" main.go

test:
	go test -v -count=1 -coverprofile=coverage.out ./...
	@echo ""
	@go tool cover -func=coverage.out | tail -1

test-html:
	go test -v -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Open coverage.html in a browser"

build:
	docker build --build-arg VERSION=$(VERSION) -t $(REPO):$(IMAGE_TAG) .

push:
	docker push $(REPO):$(IMAGE_TAG)

deploy: build push
	gcloud run deploy $(SERVICE) \
		--image $(REPO):$(IMAGE_TAG) \
		--region $(REGION) \
		--platform managed \
		--allow-unauthenticated=false \
		--set-env-vars "GOOGLE_CLOUD_PROJECT=$(GOOGLE_CLOUD_PROJECT)" \
		--set-env-vars "GOOGLE_CLOUD_LOCATION=$(REGION)" \
		--set-env-vars "ALLOWED_DOMAINS=$(ALLOWED_DOMAINS)" \
		--set-env-vars "FALLBACK_GEMINI_MODEL=$(FALLBACK_GEMINI_MODEL)" \
		--set-env-vars "RESPONSE_MODE=$(RESPONSE_MODE)" \
		--set-env-vars "MODEL_ARMOR_TEMPLATE=$(MODEL_ARMOR_TEMPLATE)" \
		--set-env-vars "MODEL_ARMOR_LOCATION=$(REGION)"

run-local: build
	@echo "Starting bulwarkai on http://localhost:8080"
	docker run --rm -p 8080:8080 \
		--env-file .env \
		--name bulwarkai \
		$(REPO):$(IMAGE_TAG)

health:
	@curl -s http://localhost:8080/health | jq . 2>/dev/null || curl -s http://localhost:8080/health

clean:
	rm -f coverage.out coverage.html

sast:
	gosec -fmt text -quiet ./...

scan: sast
	govulncheck ./...

docs:
	swag init -g main.go -d .,./internal/handler --parseInternal --output ./docs --outputTypes yaml
