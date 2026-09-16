# Hermetrix harness — everyday commands, one name each.
# Full suites stay explicit; these are the four you reach for daily.

DATA ?= ./.hermetrix
LISTEN ?= 127.0.0.1:7331

.PHONY: run test smoke backup

run:
	go run ./cmd/hermetrix serve --data $(DATA) --listen $(LISTEN)

test:
	go test ./...
	node --test internal/web/ui/runtime.test.js
	node --check internal/web/ui/app.js
	./scripts/doc-truth.sh check

smoke:
	./scripts/smoke.sh http://$(LISTEN)

backup:
	@mkdir -p backups
	sqlite3 $(DATA)/hermetrix.db ".backup 'backups/hermetrix-$$(date +%F-%H%M).db'"
	@echo "backup written to backups/"
