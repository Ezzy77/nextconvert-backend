#!/bin/bash
# Smoke tests for Convert Studio API

set -e

API_URL="${API_URL:-http://localhost:8080}"
MAX_RETRIES=30
RETRY_DELAY=2

echo "Running smoke tests against $API_URL"

# Function to wait for service to be ready
wait_for_service() {
    echo "Waiting for service to be ready..."
    for i in $(seq 1 $MAX_RETRIES); do
        if curl -f -s "$API_URL/api/v1/health" > /dev/null 2>&1; then
            echo "Service is ready!"
            return 0
        fi
        echo "Attempt $i/$MAX_RETRIES: Service not ready yet, retrying in ${RETRY_DELAY}s..."
        sleep $RETRY_DELAY
    done
    echo "ERROR: Service failed to become ready after $MAX_RETRIES attempts"
    return 1
}

# Test 1: Health check
test_health() {
    echo "Test 1: Health check"
    response=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/health")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)
    
    if [ "$http_code" -eq 200 ]; then
        echo "✓ Health check passed"
        return 0
    else
        echo "✗ Health check failed (HTTP $http_code)"
        echo "Response: $body"
        return 1
    fi
}

# Test 2: Readiness check
test_ready() {
    echo "Test 2: Readiness check"
    response=$(curl -s -w "\n%{http_code}" "$API_URL/api/v1/ready")
    http_code=$(echo "$response" | tail -n1)
    body=$(echo "$response" | head -n-1)
    
    if [ "$http_code" -eq 200 ]; then
        echo "✓ Readiness check passed"
        echo "Response: $body"
        return 0
    else
        echo "✗ Readiness check failed (HTTP $http_code)"
        echo "Response: $body"
        return 1
    fi
}

# Test 3: CORS headers
test_cors() {
    echo "Test 3: CORS headers"
    response=$(curl -s -I -X OPTIONS "$API_URL/api/v1/health")
    
    if echo "$response" | grep -q "Access-Control-Allow-Origin"; then
        echo "✓ CORS headers present"
        return 0
    else
        echo "✗ CORS headers missing"
        return 1
    fi
}

# Test 4: Invalid endpoint returns 404
test_404() {
    echo "Test 4: 404 handling"
    http_code=$(curl -s -o /dev/null -w "%{http_code}" "$API_URL/api/v1/nonexistent")
    
    if [ "$http_code" -eq 404 ]; then
        echo "✓ 404 handling works correctly"
        return 0
    else
        echo "✗ Expected 404, got $http_code"
        return 1
    fi
}

# Test 5: API version endpoint exists
test_version() {
    echo "Test 5: API structure check"
    response=$(curl -s "$API_URL/api/v1/health")
    
    if [ -n "$response" ]; then
        echo "✓ API v1 endpoint responds"
        return 0
    else
        echo "✗ API v1 endpoint not responding"
        return 1
    fi
}

# Main execution
main() {
    local failed=0
    
    # Wait for service
    if ! wait_for_service; then
        echo "ERROR: Service not available"
        exit 1
    fi
    
    echo ""
    echo "========================================="
    echo "Starting smoke tests..."
    echo "========================================="
    echo ""
    
    # Run tests
    test_health || ((failed++))
    echo ""
    
    test_ready || ((failed++))
    echo ""
    
    test_cors || ((failed++))
    echo ""
    
    test_404 || ((failed++))
    echo ""
    
    test_version || ((failed++))
    echo ""
    
    # Summary
    echo "========================================="
    if [ $failed -eq 0 ]; then
        echo "✓ All smoke tests passed!"
        echo "========================================="
        exit 0
    else
        echo "✗ $failed test(s) failed"
        echo "========================================="
        exit 1
    fi
}

main
