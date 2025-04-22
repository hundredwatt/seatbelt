.PHONY: all

psql:
	docker exec -it postgres_sink psql -U postgres -d sling

mysql:
	docker exec -it mysql_source mysql -u mysqluser -pmysqlpw -D mysql_db

sling_run:
	sling run -r sling-conf/replication.yml 2>&1 | tee -a data/sling/sling.log
	@if [ $$? -eq 0 ]; then \
		mkdir -p data/sling && touch data/sling/last_succeeded; \
	fi

seatbelt_check:
	@if [ -f data/seatbelt/last_succeeded ] && [ -f data/sling/last_succeeded ] && [ data/seatbelt/last_succeeded -nt data/sling/last_succeeded ]; then \
		echo "Error: Sling must succeed before seatbelt can run again."; \
		exit 1; \
	fi
	@echo "./seatbelt -r sling-conf/replication.yml"
	@python3 seatbelt/check.py
	@if [ $$? -eq 0 ]; then \
		mkdir -p data/seatbelt && touch data/seatbelt/last_succeeded; \
	fi

seatbelt_mark_as_repaired:
	@if [ -z "$$IDS" ]; then \
		echo "Error: IDS environment variable must be set. Example: IDS=1,2,3 make seatbelt_mark_as_repaired"; \
		exit 1; \
	fi
	@echo "./seatbelt mark_as_repaired -r sling-conf/replication.yml"
	@python3 seatbelt/mark_as_repaired.py 

seatbelt_reset:
	@rm -rf data/seatbelt/*
	@if [ -f data/sling/last_succeeded ]; then \
		rm data/sling/last_succeeded; \
	fi
	@mkdir -p data/seatbelt
