DATABASE_URL ?= postgres://shop:shop@localhost:5435/shop?sslmode=disable

.PHONY: db seed test

db:
	docker compose up -d --wait

seed:
	DATABASE_URL="$(DATABASE_URL)" go run ./cmd/shopstats seed

test:
	TEST_DATABASE_URL="$(DATABASE_URL)" go test -count=1 ./...
