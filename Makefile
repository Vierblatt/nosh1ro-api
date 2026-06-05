.PHONY: run test vet build build-linux deploy clean

# Local development
run:
	go run ./cmd/api/

# Run all tests with race detection
test:
	go test ./... -race -shuffle=on -count=1

# Run go vet
vet:
	go vet ./...

# Build for current OS
build:
	go build -o server ./cmd/api/

# Cross-compile for Linux amd64 (production)
build-linux:
	GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o server ./cmd/api/

# Deploy to production server
deploy: build-linux
	ssh root@139.159.232.200 "systemctl stop blog-api"
	scp server encryption.json go-plan-encryption.json root@139.159.232.200:/opt/blog-api/
	ssh root@139.159.232.200 "systemctl start blog-api && sleep 2 && systemctl status blog-api --no-pager"

# Remove build artifacts
clean:
	rm -f server server.exe coverage.out
