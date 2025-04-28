APP_NAME = go-flow
SRC = .
BIN_DIR = bin
BINARY_LINUX = $(BIN_DIR)/$(APP_NAME)
BINARY_WIN = $(BIN_DIR)/$(APP_NAME).exe
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
build: build-centos
	@echo "Default build completed (CentOS)."

.PHONY: build-centos
build-centos: install-deps
	@echo "Building $(APP_NAME) for CentOS..."
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=1 GOOS=linux GOARCH=amd64 $(GO) build -a -o $(BINARY_LINUX) $(SRC)

.PHONY: build-win
build-win:
	@echo "Building $(APP_NAME) for Windows..."
	@mkdir -p $(BIN_DIR)
	@CGO_ENABLED=1 GOOS=windows GOARCH=amd64 $(GO) build -a -o $(BINARY_WIN) $(SRC)

.PHONY: clean
clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR)