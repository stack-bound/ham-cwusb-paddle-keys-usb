VERSION := $(shell tr -d '[:space:]' < VERSION)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build run install-local snapshot clean

build:
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o paddlekeys .

run: build
	./paddlekeys -v

# Build all packages locally into dist/ without publishing (needs goreleaser).
snapshot:
	goreleaser release --snapshot --clean

clean:
	rm -rf paddlekeys dist
