-- Convert Studio Database Schema
-- Version: 1.0

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Enum Types
CREATE TYPE user_tier AS ENUM ('free', 'pro', 'enterprise');
CREATE TYPE file_zone AS ENUM ('upload', 'working', 'output');
CREATE TYPE job_type AS ENUM ('media', 'document');
CREATE TYPE job_status AS ENUM ('pending', 'queued', 'processing', 'completed', 'failed', 'cancelled');
CREATE TYPE preset_type AS ENUM ('video', 'audio', 'image');
CREATE TYPE document_format AS ENUM ('pdf', 'docx', 'html', 'epub', 'md', 'latex', 'odt', 'rtf', 'txt');

-- Users Table
CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash TEXT NOT NULL,
    tier user_tier DEFAULT 'free',
    quota_bytes BIGINT DEFAULT 5368709120, -- 5GB
    quota_used BIGINT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Files Table
CREATE TABLE IF NOT EXISTS files (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    original_name TEXT NOT NULL,
    storage_path TEXT NOT NULL,
    mime_type VARCHAR(255),
    size_bytes BIGINT NOT NULL,
    zone file_zone NOT NULL,
    metadata JSONB DEFAULT '{}',
    checksum VARCHAR(64),
    expires_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Jobs Table
CREATE TABLE IF NOT EXISTS jobs (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    type job_type NOT NULL,
    status job_status DEFAULT 'pending',
    priority INT DEFAULT 5,
    input_file_id UUID REFERENCES files(id),
    output_file_id UUID REFERENCES files(id),
    output_file_name TEXT,
    operations JSONB DEFAULT '[]',
    progress JSONB DEFAULT '{"percent": 0}',
    error JSONB,
    retry_count INT DEFAULT 0,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE
);

-- Presets Table
CREATE TABLE IF NOT EXISTS presets (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    type preset_type NOT NULL,
    description TEXT,
    operations JSONB NOT NULL,
    is_system BOOLEAN DEFAULT FALSE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Templates Table
CREATE TABLE IF NOT EXISTS templates (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    name VARCHAR(100) NOT NULL,
    format document_format NOT NULL,
    description TEXT,
    content TEXT,
    is_system BOOLEAN DEFAULT FALSE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Workflows Table
CREATE TABLE IF NOT EXISTS workflows (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(100) NOT NULL,
    description TEXT,
    steps JSONB NOT NULL DEFAULT '[]',
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- Indexes
CREATE INDEX idx_files_user_id ON files(user_id);
CREATE INDEX idx_files_zone ON files(zone);
CREATE INDEX idx_files_expires_at ON files(expires_at) WHERE expires_at IS NOT NULL;

CREATE INDEX idx_jobs_user_id ON jobs(user_id);
CREATE INDEX idx_jobs_status ON jobs(status);
CREATE INDEX idx_jobs_created_at ON jobs(created_at DESC);

CREATE INDEX idx_presets_user_id ON presets(user_id);
CREATE INDEX idx_presets_type ON presets(type);

CREATE INDEX idx_templates_format ON templates(format);

-- Insert default presets
INSERT INTO presets (id, name, type, description, operations, is_system) VALUES
    (uuid_generate_v4(), 'Mobile Optimized', 'video', 'Optimized for mobile devices (720p, H.264)', 
     '[{"type":"resize","params":{"width":1280,"height":720,"maintainAspect":true}},{"type":"compress","params":{"quality":70}},{"type":"convertFormat","params":{"targetFormat":"mp4","codec":"h264"}}]', 
     TRUE),
    (uuid_generate_v4(), 'Web Optimized', 'video', 'Optimized for web streaming (1080p, WebM)',
     '[{"type":"resize","params":{"width":1920,"height":1080,"maintainAspect":true}},{"type":"compress","params":{"quality":80}},{"type":"convertFormat","params":{"targetFormat":"webm","codec":"vp9"}}]',
     TRUE),
    (uuid_generate_v4(), 'Email Attachment', 'video', 'Small file size for email (<25MB target)',
     '[{"type":"resize","params":{"width":640,"height":480,"maintainAspect":true}},{"type":"compress","params":{"targetSize":25000000}},{"type":"convertFormat","params":{"targetFormat":"mp4","codec":"h264"}}]',
     TRUE),
    (uuid_generate_v4(), 'Podcast Audio', 'audio', 'Optimized for podcast distribution (MP3, 128kbps)',
     '[{"type":"convertFormat","params":{"targetFormat":"mp3"}},{"type":"changeBitrate","params":{"bitrate":128000}}]',
     TRUE),
    (uuid_generate_v4(), 'Create GIF', 'video', 'Convert video clip to animated GIF',
     '[{"type":"createGif","params":{"fps":10,"width":480}}]',
     TRUE);

-- Insert default templates
INSERT INTO templates (id, name, format, description, is_system) VALUES
    (uuid_generate_v4(), 'Academic Paper', 'pdf', 'Professional academic paper format with proper margins and citations', TRUE),
    (uuid_generate_v4(), 'Business Report', 'pdf', 'Corporate report format with executive summary section', TRUE),
    (uuid_generate_v4(), 'Simple HTML', 'html', 'Clean, minimal HTML output', TRUE),
    (uuid_generate_v4(), 'Resume/CV', 'pdf', 'Professional resume format', TRUE);

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger for users table
CREATE TRIGGER update_users_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger for workflows table
CREATE TRIGGER update_workflows_updated_at
    BEFORE UPDATE ON workflows
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
