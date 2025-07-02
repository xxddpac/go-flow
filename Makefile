APP_NAME = go-flow
SRC = .
BIN_DIR = bin
BINARY_LINUX = $(BIN_DIR)/$(APP_NAME)
GO = go

LIBPCAP_VERSION = 1.10.1
LIBPCAP_TAR = libpcap-$(LIBPCAP_VERSION).tar.gz
LIBPCAP_DIR = libpcap-$(LIBPCAP_VERSION)
LIBPCAP_URL = https://www.tcpdump.org/release/$(LIBPCAP_TAR)
LIBPCAP_PREFIX = /usr/local

.PHONY: all
all: build

.PHONY: install-build-deps
install-build-deps:
	@echo "Installing build dependencies..."
	@sudo yum install -y gcc make autoconf automake libtool wget curl flex bison || true

.PHONY: install-libpcap-static
install-libpcap-static: install-build-deps
	@if [ ! -f "$(LIBPCAP_PREFIX)/lib/libpcap.a" ]; then \
		echo "libpcap static library not found, building from source..."; \
		if [ ! -d "$(LIBPCAP_DIR)" ]; then \
			echo "Downloading libpcap..."; \
			curl -LO $(LIBPCAP_URL); \
			tar xzf $(LIBPCAP_TAR); \
		fi; \
		cd $(LIBPCAP_DIR) && ./configure --enable-static --disable-shared --prefix=$(LIBPCAP_PREFIX) && make && sudo make install; \
	else \
		echo "libpcap static library already installed."; \
	fi

.PHONY: build
build: install-libpcap-static
	@echo "Building $(APP_NAME) for Linux (static)..."
	@mkdir -p $(BIN_DIR)
	@CGO_LDFLAGS="-L$(LIBPCAP_PREFIX)/lib" CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
		$(GO) build -a -ldflags '-extldflags "-static"' -o $(BINARY_LINUX) $(SRC)

.PHONY: clean
clean:
	@echo "Cleaning..."
	@rm -rf $(BIN_DIR) $(LIBPCAP_DIR) $(LIBPCAP_TAR)
