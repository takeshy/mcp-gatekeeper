BINARY_NAME=mcp-gatekeeper
ADMIN_BINARY_NAME=mcp-gatekeeper-admin
DIST_DIR=dist
VERSION=0.1.0
LDFLAGS=-ldflags "-X github.com/takeshy/mcp-gatekeeper/internal/version.Version=$(VERSION)"

.PHONY: all clean build release test

all: build

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/server
	go build $(LDFLAGS) -o $(ADMIN_BINARY_NAME) ./cmd/admin

test:
	go test -v ./...

clean:
	rm -rf $(DIST_DIR)
	rm -f $(BINARY_NAME) $(ADMIN_BINARY_NAME)

release: clean
	mkdir -p $(DIST_DIR)
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/server
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(ADMIN_BINARY_NAME)-linux-amd64 ./cmd/admin
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/server
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(ADMIN_BINARY_NAME)-linux-arm64 ./cmd/admin
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/server
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(ADMIN_BINARY_NAME)-darwin-amd64 ./cmd/admin
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/server
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(ADMIN_BINARY_NAME)-darwin-arm64 ./cmd/admin
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/server
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(DIST_DIR)/$(ADMIN_BINARY_NAME)-windows-amd64.exe ./cmd/admin
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(BINARY_NAME)-windows-arm64.exe ./cmd/server
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o $(DIST_DIR)/$(ADMIN_BINARY_NAME)-windows-arm64.exe ./cmd/admin
