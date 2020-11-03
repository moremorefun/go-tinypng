# Binary name
BINARY=tinypng
# Builds the project
build:
		go build -o bin/${BINARY} cmd/main.go
# Installs our project: copies binaries
install:
		go install
release:
		# Clean
		go clean
		rm -rf bin/*tinypng*
		# Build for mac
		CGO_ENABLED=0 GOOS=darwin GOARCH=amd64 go build -o bin/mac-${BINARY}-${VERSION} cmd/main.go
		# Build for linux
		CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build  -o bin/linux-${BINARY}-${VERSION} cmd/main.go
		# Build for win
		CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o bin/win-${BINARY}-${VERSION}.exe cmd/main.go
		go clean
# Cleans our projects: deletes binaries
clean:
		go clean

.PHONY:  clean build