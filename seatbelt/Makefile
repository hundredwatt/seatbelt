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

clean:
	rm -rf build
	rm -rf tmp
	mkdir -p tmp

all: clean install copy-to-mini
