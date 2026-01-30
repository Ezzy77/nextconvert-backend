-- Migration: Change user_id from UUID to TEXT for Clerk compatibility
-- Clerk user IDs are strings like "user_2abc123xyz", not UUIDs

-- Drop foreign key constraints
ALTER TABLE files DROP CONSTRAINT IF EXISTS files_user_id_fkey;
ALTER TABLE jobs DROP CONSTRAINT IF EXISTS jobs_user_id_fkey;
ALTER TABLE presets DROP CONSTRAINT IF EXISTS presets_user_id_fkey;

-- Change user_id columns from UUID to TEXT
ALTER TABLE files ALTER COLUMN user_id TYPE TEXT USING user_id::TEXT;
ALTER TABLE jobs ALTER COLUMN user_id TYPE TEXT USING user_id::TEXT;
ALTER TABLE presets ALTER COLUMN user_id TYPE TEXT USING user_id::TEXT;

-- We no longer need the users table for auth (Clerk handles it)
-- But keep it for now in case we need to store additional user data
-- Just change the id column to TEXT
ALTER TABLE users ALTER COLUMN id TYPE TEXT USING id::TEXT;

-- Allow NULL user_id for anonymous uploads
ALTER TABLE files ALTER COLUMN user_id DROP NOT NULL;
ALTER TABLE jobs ALTER COLUMN user_id DROP NOT NULL;
