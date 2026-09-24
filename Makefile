# hoster: build, test e controlli della documentazione.
#
#   make           ./hoster, il binario statico Linux x86-64 registrato nel repository
#   make test      go vet, gofmt, test con -race, controlli dei manuali
#   make docs-check solo i controlli dei manuali
#   make dist      binari statici per Linux amd64 e arm64 in dist/
#   make live      test dal vivo con account e link reali (HOSTER_LIVE=1)
#   make clean     elimina dist/ (./hoster resta: fa parte del repository)

GO      ?= go
LDFLAGS := -s -w
VERSION := $(shell sed -n 's/.*"hoster \([0-9.]*\)".*/\1/p' help.go)

.PHONY: all build test vet fmt-check docs-check dist live clean

all: build

build:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o hoster .

vet:
	$(GO) vet ./...

fmt-check:
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "file da formattare con gofmt:"; echo "$$out"; exit 1; fi

docs-check:
	python3 tools/check-docs.py

test: vet fmt-check docs-check
	$(GO) test -race -count=1 ./...

dist:
	@mkdir -p dist
	for arch in amd64 arm64; do \
	  CGO_ENABLED=0 GOOS=linux GOARCH=$$arch $(GO) build -trimpath -ldflags "$(LDFLAGS)" \
	    -o dist/hoster-linux-$$arch . || exit 1; \
	done
	cd dist && sha256sum hoster-linux-* > SHA256SUMS && cat SHA256SUMS

live:
	HOSTER_LIVE=1 $(GO) test -run TestLiveResolve -v -count=1 ./...

clean:
	rm -rf dist
