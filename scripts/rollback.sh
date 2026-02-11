#!/bin/bash
# Rollback NextConvert deployment

set -e

echo "========================================="
echo "Rollback Deployment"
echo "========================================="

# Configuration
COMPOSE_FILE="${COMPOSE_FILE:-docker-compose.yml}"
ENVIRONMENT="${ENVIRONMENT:-staging}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Confirm rollback
confirm_rollback() {
    log_warn "========================================="
    log_warn "WARNING: Rollback Operation"
    log_warn "========================================="
    log_warn "Environment: $ENVIRONMENT"
    log_warn ""
    
    read -p "Are you sure you want to rollback? (yes/no): " -r
    echo
    if [[ ! $REPLY =~ ^[Yy][Ee][Ss]$ ]]; then
        log_error "Rollback cancelled by user"
        exit 1
    fi
    
    log_info "Rollback confirmed"
}

# Find latest backup
find_backup() {
    if [ -f .last_backup ]; then
        BACKUP_DIR=$(cat .last_backup)
        if [ -d "$BACKUP_DIR" ]; then
            log_info "Found backup at $BACKUP_DIR"
            return 0
        fi
    fi
    
    # Try to find latest backup
    BACKUP_DIR=$(ls -td backups/*/ 2>/dev/null | head -n1)
    if [ -z "$BACKUP_DIR" ]; then
        log_error "No backup found"
        return 1
    fi
    
    log_info "Using backup at $BACKUP_DIR"
    return 0
}

# Restore configuration
restore_config() {
    log_info "Restoring configuration..."
    
    if [ -f "$BACKUP_DIR/.env.backup" ]; then
        cp "$BACKUP_DIR/.env.backup" .env
        log_info "Configuration restored"
    else
        log_warn "No configuration backup found"
    fi
}

# Restore database
restore_database() {
    log_info "Restoring database..."
    
    if [ -f "$BACKUP_DIR/database.sql" ]; then
        # Add database restore command
        # Example: psql -h localhost -U postgres nextconvert < "$BACKUP_DIR/database.sql"
        log_info "Database restore skipped (implement if needed)"
    else
        log_warn "No database backup found"
    fi
}

# Revert to previous images
revert_images() {
    log_info "Reverting to previous Docker images..."
    
    if [ -f "$BACKUP_DIR/images.txt" ]; then
        # Extract previous image tag
        PREVIOUS_TAG=$(grep "server" "$BACKUP_DIR/images.txt" | awk '{print $2}' | cut -d':' -f2 | head -n1)
        
        if [ -n "$PREVIOUS_TAG" ]; then
            log_info "Rolling back to version: $PREVIOUS_TAG"
            export IMAGE_TAG="$PREVIOUS_TAG"
            
            docker-compose -f "$COMPOSE_FILE" pull
            docker-compose -f "$COMPOSE_FILE" up -d
            
            log_info "Images reverted successfully"
        else
            log_error "Could not determine previous version"
            return 1
        fi
    else
        log_error "No image backup found"
        return 1
    fi
}

# Verify rollback
verify_rollback() {
    log_info "Verifying rollback..."
    
    sleep 10
    
    if curl -f -s http://localhost:8080/api/v1/health > /dev/null 2>&1; then
        log_info "Service health check passed"
        return 0
    else
        log_error "Service health check failed"
        return 1
    fi
}

# Main rollback flow
main() {
    log_info "Starting rollback process..."
    echo ""
    
    confirm_rollback
    echo ""
    
    if ! find_backup; then
        log_error "Cannot proceed without backup"
        exit 1
    fi
    echo ""
    
    restore_config
    echo ""
    
    # Uncomment if database restore is needed
    # restore_database
    # echo ""
    
    if ! revert_images; then
        log_error "Failed to revert images"
        exit 1
    fi
    echo ""
    
    if ! verify_rollback; then
        log_error "Rollback verification failed"
        exit 1
    fi
    echo ""
    
    log_info "========================================="
    log_info "✓ Rollback completed successfully!"
    log_info "Version: $PREVIOUS_TAG"
    log_info "========================================="
}

main
