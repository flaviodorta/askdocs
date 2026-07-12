# One command per task. Migrations run via the migrate/migrate docker image so
# no local install of golang-migrate is required.

DATABASE_URL ?= postgres://askdocs:askdocs@localhost:5433/askdocs?sslmode=disable
MIGRATIONS_DIR := backend/migrations
MIGRATE := docker run --rm -v $(CURDIR)/$(MIGRATIONS_DIR):/migrations --network host \
	migrate/migrate -path=/migrations -database "$(DATABASE_URL)"

.PHONY: db-up db-down migrate-up migrate-down migrate-new

db-up: ## start Postgres+pgvector and wait until healthy
	docker compose up -d --wait

db-down: ## stop local infra (data volume is kept)
	docker compose down

migrate-up: ## apply all pending migrations
	$(MIGRATE) up

migrate-down: ## roll back the most recent migration
	$(MIGRATE) down 1

migrate-new: ## create a new migration: make migrate-new name=create_documents
	docker run --rm -v $(CURDIR)/$(MIGRATIONS_DIR):/migrations \
		migrate/migrate create -ext sql -dir /migrations -seq $(name)
