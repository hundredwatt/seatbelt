.PHONY: build build-intel install copy-to-mini clean

build: ensure-build-dir
	go build -o build/seatbelt ./cmd/seatbelt

build-intel: ensure-build-dir
	GOOS=darwin GOARCH=amd64 go build -o build/seatbelt-intel ./cmd/seatbelt

install:
	go install -o build/seatbelt ./cmd/seatbelt

copy-to-mini: build-intel
	scp build/seatbelt-intel macmini.lan:~/seatbelt
	ssh macmini.lan "chmod +x ~/seatbelt && sudo mv ~/seatbelt /usr/local/bin/"

ensure-build-dir:
	mkdir -p build

clean:
	rm -rf build

all: clean install copy-to-mini
