.PHONY: all

up:
	docker-compose -f test/docker-compose.yml up -d

down:
	docker-compose -f test/docker-compose.yml down

clean:
	docker-compose -f test/docker-compose.yml down
	rm -rf test/.postgres_data
	rm -rf test/.clickhouse_data

logs:
	docker-compose -f test/docker-compose.yml logs -f

psql:
	docker exec -it seatbelt-postgres psql -U postgres -d seatbelt

clickhouse-client:
	docker exec -it seatbelt-clickhouse clickhouse-client -d peerdb --password pass

test:
	go test -v ./...

build:
	go build -o seatbelt ./cmd/seatbelt/main.go
