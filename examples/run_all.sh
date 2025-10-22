#!/bin/bash

set -e

echo "Starting Floxy Examples..."

# Check if database is running
echo "Checking database connection..."
if ! pg_isready -h localhost -p 5435 -U user -d floxy > /dev/null 2>&1; then
    echo "Database is not running. Please start it with:"
    echo "cd dev && docker-compose up -d"
    exit 1
fi

echo "Database is running. Starting examples..."

# Function to run example
run_example() {
    local example_name=$1
    local example_dir=$2
    
    echo ""
    echo "=========================================="
    echo "Running $example_name example..."
    echo "=========================================="
    
    cd "$example_dir"
    
    echo "Installing dependencies..."
    go mod tidy
    
    echo "Starting $example_name..."
    timeout 30s go run main.go || true
    
    cd ..
    echo "Finished $example_name example"
}

# Run examples in order of complexity
run_example "Hello World" "hello_world"
run_example "E-commerce" "ecommerce"
run_example "Data Pipeline" "data_pipeline"
run_example "Microservices" "microservices"

echo ""
echo "=========================================="
echo "All examples completed!"
echo "=========================================="
