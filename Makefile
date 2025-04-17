APP_NAME = go-flow
SRC = main.go
BIN_DIR = bin
BINARY = $(BIN_DIR)/$(APP_NAME)
GO = go
YUM = sudo yum
PKG = gcc libpcap-devel

.PHONY: all
all: build

.PHONY: install-deps
install-deps:
	@echo "Installing dependencies..."
	@$(YUM) install -y $(PKG) || { echo "Yum failed, trying apt..."; sudo apt-get update && sudo apt-get install -y gcc libpcap-dev; }

.PHONY: build
build: install-deps
	@echo "Building $(APP_NAME)..."
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=1 GOOS=linux GOARCH=amd64 $(GO) build -a -o $(BINARY) $(SRC)

.PHONY: clean
clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)
