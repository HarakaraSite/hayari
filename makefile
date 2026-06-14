BINARY = yarr2
VERSION ?= dev

.PHONY: build build-gui run clean test

build:
	go build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) ./cmd/yarr2

build-gui:
	go build -tags gui -ldflags "-X main.Version=$(VERSION)" -o $(BINARY)-gui ./cmd/yarr2

run:
	go run ./cmd/yarr2

test:
	go test ./...

clean:
	rm -f $(BINARY) $(BINARY)-gui
