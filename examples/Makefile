.PHONY: help hello ecommerce data-pipeline microservices all clean

help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

hello: ## Run hello world example
	cd hello_world && go mod tidy && go run main.go

ecommerce: ## Run e-commerce example
	cd ecommerce && go mod tidy && go run main.go

data-pipeline: ## Run data pipeline example
	cd data_pipeline && go mod tidy && go run main.go

microservices: ## Run microservices example
	cd microservices && go mod tidy && go run main.go

all: ## Run all examples
	./run_all.sh

clean: ## Clean up go modules
	find . -name "go.sum" -delete
	find . -name "go.mod" -not -path "./go.mod" -delete

setup: ## Setup database and run migrations
	cd ../dev && docker-compose up -d
	sleep 5
	cd .. && go run cmd/migrate/main.go

stop: ## Stop database
	cd ../dev && docker-compose down
