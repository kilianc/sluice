IMAGE := sluice-tools
# Mount the repository read-only, as your own uid, with no ports. The adapter
# and the test runner only ever read.
NODE := docker run --rm -i -u "$$(id -u):$$(id -g)" -v "$$PWD:/work:ro" -w /work $(IMAGE) node

.PHONY: help test test-go test-js race conformance conformance-js tools fmt vet playground

help:
	@echo "test            everything: Go, then the JS package in the tools image"
	@echo "test-go         go test ./..."
	@echo "test-js         node --test for every JS package, in the tools image"
	@echo "playground      serve the PGlite playground at http://localhost:8901"
	@echo "race            go test -race ./..."
	@echo "conformance     run the corpus against every adapter that can run here"
	@echo "conformance-js  run the corpus against the JS adapter, in the tools image"
	@echo "tools           build the pinned toolchain image ($(IMAGE))"

test: test-go test-js

test-go:
	go test ./...

test-js: tools
	$(NODE) --test js/packages/core/test/core.test.js js/packages/monaco/test/monaco.test.js

race:
	go test -race ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

conformance:
	go test ./conformance

# The JS adapter needs a JavaScript runtime. On a host without one — or with one
# you would rather not hand a repository to — this runs it in the pinned image
# instead. The runner mounts the repository read-only and as your own uid.
conformance-js: tools
	go test ./conformance -v -run 'TestConformance/js'

tools:
	docker build -t $(IMAGE) tools/

# The playground is buildless: it loads Monaco and PGlite from a pinned CDN and
# Sluice from this working tree, so what you see is the code you have.
playground:
	@echo "http://localhost:8901/playground/"
	@python3 -m http.server 8901 --bind 127.0.0.1
