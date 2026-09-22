.PHONY: all build run test tidy clean docker-build docker-up docker-down

BINARY_NAME=bin/server
MAIN_PATH=./cmd/server

all: build

build:
	@echo "Building binary..."
	@mkdir -p bin
	@go build -o $(BINARY_NAME) $(MAIN_PATH)

run:
	@echo "Running local server..."
	@go run $(MAIN_PATH)

test:
	@echo "Running tests..."
	@go test -v -race ./...

tidy:
	@echo "Tidying go modules..."
	@go mod tidy

clean:
	@echo "Cleaning up..."
	@rm -rf bin/

docker-build:
	@echo "Building Docker image..."
	@docker build -t hack-go-thon-server .

docker-up:
	@echo "Starting services with docker compose..."
	@docker compose up -d --build

docker-down:
	@echo "Stopping docker compose services..."
	@docker compose down

