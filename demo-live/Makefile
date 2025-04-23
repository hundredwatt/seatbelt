.PHONY: all

psql:
	docker exec -it postgres_sink psql -U postgres -d sling

mysql:
	docker exec -it mysql_source mysql -u mysqluser -pmysqlpw -D mysql_db

sling_run:
	set -o pipefail; sling run -r sling-conf/replication.yml 2>&1 | tee -a data/sling/sling.log
	@if [ $$? -eq 0 ]; then \
		mkdir -p data/sling && touch data/sling/last_succeeded; \
	fi

seatbelt_check:
	@if [ -f data/seatbelt/last_succeeded ] && [ -f data/sling/last_succeeded ] && [ data/seatbelt/last_succeeded -nt data/sling/last_succeeded ]; then \
		echo "Error: Sling must succeed before seatbelt can run again."; \
		exit 1; \
	fi
	set -o pipefail; ./bin/seatbelt -c $${CONFIG_FILE:-seatbelt-conf/default.yml} $${SEATBELT_ARGS} 2>&1 | tee -a data/seatbelt/seatbelt.log
	@if [ $$? -eq 0 ]; then \
		mkdir -p data/seatbelt && touch data/seatbelt/last_succeeded; \
	fi

seatbelt_reset:
	@rm -rf data/seatbelt/*
	@if [ -f data/sling/last_succeeded ]; then \
		rm data/sling/last_succeeded; \
	fi
	@mkdir -p data/seatbelt
