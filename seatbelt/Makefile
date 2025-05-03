## All
all: clean install copy-to-mini

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

test:
	go test -v ./...

## Build/Install
# We don't really need this, as we can use `go run cmd/seatbelt/main.go` to run the program for development.
build: ensure-build-dir
	go build -o build/seatbelt ./cmd/seatbelt

build-intel: ensure-build-dir
	GOOS=darwin GOARCH=amd64 go build -o build/seatbelt-intel ./cmd/seatbelt

install:
	go install ./cmd/seatbelt

copy-to-mini: build-intel
	scp build/seatbelt-intel macmini.lan:~/seatbelt
	ssh macmini.lan "chmod +x ~/seatbelt && sudo mv ~/seatbelt /usr/local/bin/"

ensure-build-dir:
	mkdir -p build

## Clean
clean:
	docker-compose -f test/docker-compose.yml down
	rm -rf build
	rm -rf tmp
	mkdir -p tmp
