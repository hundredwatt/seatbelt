## All
all: install copy-to-mini

## Test/Development Resources
up:
	docker-compose -f test/docker-compose.yml up -d

down:
	docker-compose -f test/docker-compose.yml down

logs:
	docker-compose -f test/docker-compose.yml logs -f

psql:
	docker exec -it seatbelt-postgres psql -U postgres -d seatbelt

clickhouse-client:
	docker exec -it seatbelt-clickhouse clickhouse-client -d peerdb --password pass

.PHONY: test
test:
	go test -v ./...

## WASM row mapper
# Build the reference Zig mapper (wasm-mappers/peerdb) and vendor the artifact
# into the Go package for //go:embed. Requires Zig 0.16+.
.PHONY: wasm
wasm:
	cd ../wasm-mappers/peerdb && zig build
	cp ../wasm-mappers/peerdb/zig-out/bin/peerdb.wasm pkg/row_mappers/peerdb.wasm

## Build/Install
# We don't really need this, as we can use `go run cmd/seatbelt/main.go` to run the program for development.
build: ensure-build-dir
	git_hash=$$(git rev-parse --short HEAD) && \
	go build -ldflags="-X main.version=alpha-$$git_hash" -o build/seatbelt ./cmd/seatbelt

build-archive: build
	git_hash=$$(git rev-parse --short HEAD) && \
	build_hash=$$(md5sum build/seatbelt | cut -d' ' -f1 | head -c 12) && \
	cp build/seatbelt "build/seatbelt-archive-$$git_hash-$$build_hash"

build-intel: ensure-build-dir
	git_hash=$$(git rev-parse --short HEAD) && \
	CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -ldflags="-X main.version=alpha-$$git_hash" -o build/seatbelt-intel ./cmd/seatbelt

install:
	git_hash=$$(git rev-parse --short HEAD) && \
	go install -ldflags="-X main.version=alpha-$$git_hash" ./cmd/seatbelt

copy-to-mini: build-intel
	scp build/seatbelt-intel macmini.lan:~/seatbelt
	ssh macmini.lan "chmod +x ~/seatbelt && mv ~/seatbelt /usr/local/bin/"

ensure-build-dir:
	mkdir -p build

## Clean
clean:
	docker-compose -f test/docker-compose.yml down
	rm -rf build
	rm -rf tmp
	mkdir -p tmp
