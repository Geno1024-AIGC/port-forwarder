BINARY  := port-forwarder
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
OUTDIR  := bin
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: all test clean linux windows

all: linux windows

test:
	go test ./...

linux:
	mkdir -p $(OUTDIR)
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUTDIR)/$(BINARY)-linux-amd64 .

windows:
	mkdir -p $(OUTDIR)
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags '$(LDFLAGS)' -o $(OUTDIR)/$(BINARY)-windows-amd64.exe .

clean:
	rm -rf $(OUTDIR)