BINARY = cairo
BUILD_DIR = ./cmd/cairo
INSTALL_DIR = $(HOME)/go/bin

.PHONY: build install clean run

build:
	go build -o /tmp/$(BINARY) $(BUILD_DIR)

install:
	go install $(BUILD_DIR)

clean:
	rm -f /tmp/$(BINARY)

run: build
	/tmp/$(BINARY)
