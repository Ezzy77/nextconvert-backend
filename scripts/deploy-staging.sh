#!/bin/bash
# Deploy Convert Studio to Staging Environment

set -e

echo "========================================="
echo "Deploying to Staging Environment"
echo "========================================="

# Configuration
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
STAGING_HOST="${STAGING_HOST:-staging.convertstudio.example.com}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Pre-deployment checks
check_prerequisites() {
    log_info "Checking prerequisites..."
    
    if ! command -v docker &> /dev/null; then
        log_error "Docker is not installed"
        exit 1
    fi
    
    if ! command -v docker-compose &> /dev/null; then
        log_error "Docker Compose is not installed"
        exit 1
    fi
    
    log_info "Prerequisites check passed"
}

# Pull latest images
pull_images() {
    log_info "Pulling latest images (tag: $IMAGE_TAG)..."
    
    export IMAGE_TAG
    docker-compose -f "$COMPOSE_FILE" pull
    
    log_info "Images pulled successfully"
}

# Run database migrations
run_migrations() {
    log_info "Running database migrations..."
    
    # Add migration command here
    # Example: docker-compose exec -T server ./migrate up
    
    log_info "Migrations completed"
}

# Deploy services
deploy_services() {
    log_info "Deploying services..."
    
    export IMAGE_TAG
    docker-compose -f "$COMPOSE_FILE" up -d
    
    log_info "Services deployed"
}

# Wait for services to be healthy
wait_for_health() {
    log_info "Waiting for services to be healthy..."
    
    sleep 5
    
    max_attempts=30
    attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        if curl -f -s http://localhost:8080/api/v1/health > /dev/null 2>&1; then
            log_info "Services are healthy!"
            return 0
        fi
        
        attempt=$((attempt + 1))
        log_warn "Attempt $attempt/$max_attempts: Services not ready yet..."
        sleep 2
    done
    
    log_error "Services failed to become healthy"
    return 1
}

# Run smoke tests
run_smoke_tests() {
    log_info "Running smoke tests..."
    
    if ./scripts/smoke-test.sh; then
        log_info "Smoke tests passed"
        return 0
    else
        log_error "Smoke tests failed"
        return 1
    fi
}

# Rollback on failure
rollback() {
    log_error "Deployment failed, rolling back..."
    
    # Revert to previous version
    export IMAGE_TAG="${PREVIOUS_TAG:-stable}"
    docker-compose -f "$COMPOSE_FILE" up -d
    
    log_warn "Rolled back to previous version"
}

# Main deployment flow
main() {
    log_info "Starting deployment to staging..."
    echo ""
    
    # Save current tag for rollback
    PREVIOUS_TAG=$(docker inspect --format='{{.Config.Image}}' convert-studio-server 2>/dev/null | cut -d':' -f2 || echo "stable")
    
    check_prerequisites || exit 1
    echo ""
    
    pull_images || exit 1
    echo ""
    
    run_migrations || exit 1
    echo ""
    
    deploy_services || exit 1
    echo ""
    
    if ! wait_for_health; then
        rollback
        exit 1
    fi
    echo ""
    
    if ! run_smoke_tests; then
        rollback
        exit 1
    fi
    echo ""
    
    log_info "========================================="
    log_info "✓ Deployment to staging completed successfully!"
    log_info "Version: $IMAGE_TAG"
    log_info "URL: https://$STAGING_HOST"
    log_info "========================================="
}

main
