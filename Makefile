VERSION ?= $(shell git describe --tags --always 2>/dev/null || echo "dev")
LD_FLAGS := -w -s -X ttm/server.Version=$(VERSION)

.PHONY: build run clean

build:
	go build -trimpath -ldflags '$(LD_FLAGS)' -o ttm .

run:
	go run -ldflags '$(LD_FLAGS)' .

clean:
	rm -f ttm ttm.exe
