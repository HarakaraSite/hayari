BINARY = hayari
VERSION ?= dev

.PHONY: build build-gui run clean test

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) ./cmd/hayari

build-gui:
	go build -tags gui -ldflags "-X main.Version=$(VERSION)" -o $(BINARY)-gui ./cmd/hayari

run:
	go run ./cmd/hayari

test:
	go test ./...

clean:
	rm -f $(BINARY) $(BINARY)-gui
