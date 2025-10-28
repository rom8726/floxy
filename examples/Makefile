.PHONY: help hello ecommerce data-pipeline microservices dead-letter-queue all clean

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

savepoint: ## Run rollback to savepoint example
	cd savepoint_demo && go mod tidy && go run main.go

rollback: ## Run rollback example
	cd rollback_demo && go mod tidy && go run main.go

condition: ## Run condition example
	cd condition && go mod tidy && go run main.go

dead-letter-queue: ## Run DLQ example (move to DLQ and requeue)
	cd dead-letter-queue && go mod tidy && go run main.go
