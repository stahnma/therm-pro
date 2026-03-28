.PHONY: build run clean swag

build:
	go build -o bin/therm-pro-server ./cmd/therm-pro-server

run:
	go run ./cmd/therm-pro-server

clean:
	rm -rf bin/

swag:
	swag init -g cmd/therm-pro-server/main.go -o internal/api/docs
