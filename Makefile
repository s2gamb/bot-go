SHELL := /bin/bash

.PHONY: run

run:
	set -a && source .env && set +a && go run ./cmd/bot
