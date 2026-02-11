#!/bin/bash
# Deploy NextConvert to Production Environment

set -e

echo "========================================="
echo "Deploying to Production Environment"
echo "========================================="

# Configuration
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.prod.yml}"
IMAGE_TAG="${IMAGE_TAG:-v1.0.0}"
PRODUCTION_HOST="${PRODUCTION_HOST:-nextconvert.example.com}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
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

log_prompt() {
    echo -e "${BLUE}[PROMPT]${NC} $1"
}

# Confirmation prompt
confirm_deployment() {
    log_warn "========================================="
    log_warn "WARNING: Production Deployment"
    log_warn "========================================="
    log_warn "Environment: PRODUCTION"
    log_warn "Version: $IMAGE_TAG"
    log_warn "Host: $PRODUCTION_HOST"
    log_warn ""
    
    read -p "Are you sure you want to deploy to production? (yes/no): " -r
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        log_error "Deployment cancelled by user"
        exit 1
    fi
    
    log_info "Deployment confirmed"
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
    
    # Check if required environment variables are set
    if [ -z "$IMAGE_TAG" ]; then
        log_error "IMAGE_TAG is not set"
        exit 1
    fi
    
    log_info "Prerequisites check passed"
}

# Create backup
create_backup() {
    log_info "Creating backup..."
    
    BACKUP_DIR="backups/$(date +%Y%m%d_%H%M%S)"
    mkdir -p "$BACKUP_DIR"
    
    # Backup database
    log_info "Backing up database..."
    # Add database backup command here
    # Example: pg_dump -h localhost -U postgres nextconvert > "$BACKUP_DIR/database.sql"
    
    # Backup configuration
    log_info "Backing up configuration..."
    cp .env "$BACKUP_DIR/.env.backup" 2>/dev/null || true
    
    # Save current image tags
    docker-compose images | tee "$BACKUP_DIR/images.txt"
    
    log_info "Backup created at $BACKUP_DIR"
    echo "$BACKUP_DIR" > .last_backup
}

# Pull latest images
pull_images() {
    log_info "Pulling production images (tag: $IMAGE_TAG)..."
    
    export IMAGE_TAG
    docker-compose -f "$COMPOSE_FILE" pull
    
    log_info "Images pulled successfully"
}

# Run database migrations
run_migrations() {
    log_info "Running database migrations..."
    
    # Add migration command here
    # Example: docker-compose -f "$COMPOSE_FILE" run --rm server ./migrate up
    
    log_info "Migrations completed"
}

# Deploy services with zero-downtime
deploy_services() {
    log_info "Deploying services with zero-downtime strategy..."
    
    export IMAGE_TAG
    
    # Deploy worker first
    log_info "Deploying worker..."
    docker-compose -f "$COMPOSE_FILE" up -d worker
    sleep 5
    
    # Deploy server with rolling update
    log_info "Deploying server..."
    docker-compose -f "$COMPOSE_FILE" up -d --no-deps server
    
    log_info "Services deployed"
}

# Wait for services to be healthy
wait_for_health() {
    log_info "Waiting for services to be healthy..."
    
    sleep 10
    
    max_attempts=60
    attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        if curl -f -s "https://$PRODUCTION_HOST/api/v1/health" > /dev/null 2>&1; then
            log_info "Services are healthy!"
            return 0
        fi
        
        attempt=$((attempt + 1))
        if [ $((attempt % 10)) -eq 0 ]; then
            log_warn "Attempt $attempt/$max_attempts: Services not ready yet..."
        fi
        sleep 2
    done
    
    log_error "Services failed to become healthy"
    return 1
}

# Run smoke tests
run_smoke_tests() {
    log_info "Running smoke tests..."
    
    API_URL="https://$PRODUCTION_HOST" ./scripts/smoke-test.sh
    
    if [ $? -eq 0 ]; then
        log_info "Smoke tests passed"
        return 0
    else
        log_error "Smoke tests failed"
        return 1
    fi
}

# Post-deployment verification
verify_deployment() {
    log_info "Running post-deployment verification..."
    
    # Check service logs for errors
    log_info "Checking service logs..."
    docker-compose -f "$COMPOSE_FILE" logs --tail=50 server | grep -i error || true
    
    # Verify database connectivity
    log_info "Verifying database connectivity..."
    curl -f -s "https://$PRODUCTION_HOST/api/v1/ready" > /dev/null
    
    # Check worker is processing jobs
    log_info "Checking worker status..."
    docker-compose -f "$COMPOSE_FILE" logs --tail=20 worker
    
    log_info "Verification completed"
}

# Rollback on failure
rollback() {
    log_error "========================================="
    log_error "Deployment failed, initiating rollback..."
    log_error "========================================="
    
    if [ -f .last_backup ]; then
        BACKUP_DIR=$(cat .last_backup)
        log_info "Restoring from backup: $BACKUP_DIR"
        
        # Restore configuration
        if [ -f "$BACKUP_DIR/.env.backup" ]; then
            cp "$BACKUP_DIR/.env.backup" .env
        fi
        
        # Revert to previous images
        PREVIOUS_TAG=$(grep "server" "$BACKUP_DIR/images.txt" | awk '{print $2}' | cut -d':' -f2 | head -n1)
        if [ -n "$PREVIOUS_TAG" ]; then
            export IMAGE_TAG="$PREVIOUS_TAG"
            docker-compose -f "$COMPOSE_FILE" up -d
        fi
    fi
    
    log_warn "Rollback completed"
}

# Cleanup old images
cleanup() {
    log_info "Cleaning up old Docker images..."
    docker image prune -f
    log_info "Cleanup completed"
}

# Main deployment flow
main() {
    log_info "Starting production deployment..."
    echo ""
    
    confirm_deployment
    echo ""
    
    check_prerequisites || exit 1
    echo ""
    
    create_backup || exit 1
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
    
    verify_deployment || log_warn "Verification had warnings"
    echo ""
    
    cleanup
    echo ""
    
    log_info "========================================="
    log_info "✓ Production deployment completed successfully!"
    log_info "Version: $IMAGE_TAG"
    log_info "URL: https://$PRODUCTION_HOST"
    log_info "========================================="
    
    # Send notification
    log_info "Sending deployment notification..."
    # Add notification logic here (Slack, email, etc.)
}

main
