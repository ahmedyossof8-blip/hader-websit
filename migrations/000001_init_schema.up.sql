-- Phase 1: Core Database Schema Migration for Hader-V2
-- Extensions
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- Ensure auth schema exists (default in Supabase, ensures portability across standalone PG instances)
CREATE SCHEMA IF NOT EXISTS auth;
CREATE TABLE IF NOT EXISTS auth.users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid()
);

-- 1. Academic Departments
CREATE TABLE IF NOT EXISTS academic_departments (
    id SERIAL PRIMARY KEY,
    name VARCHAR(150) NOT NULL,
    year_level INT NOT NULL DEFAULT 2,
    faculty VARCHAR(150) NOT NULL DEFAULT 'كلية التربية النوعية'
);

-- 2. User Profiles (Extends auth.users with Hader-V2 academic identity)
CREATE TABLE IF NOT EXISTS user_profiles (
    id UUID PRIMARY KEY REFERENCES auth.users(id) ON DELETE CASCADE,
    academic_id VARCHAR(50) UNIQUE NOT NULL,
    full_name VARCHAR(200) NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    role VARCHAR(30) NOT NULL CHECK (role IN ('student', 'class_leader', 'admin')),
    department_id INT REFERENCES academic_departments(id),
    current_streak INT DEFAULT 0,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 3. User Hardware Devices (Device Pinning & Hardware Lock)
CREATE TABLE IF NOT EXISTS user_devices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES user_profiles(id) ON DELETE CASCADE,
    device_id VARCHAR(128) NOT NULL,
    device_name VARCHAR(150),
    is_trusted BOOLEAN DEFAULT TRUE,
    last_login_at TIMESTAMPTZ DEFAULT NOW(),
    last_ip INET,
    last_lat DOUBLE PRECISION,
    last_lng DOUBLE PRECISION,
    CONSTRAINT unique_user_device UNIQUE (user_id, device_id)
);

-- 4. Lecture Sessions (Lifecycle & Dynamic Attendance Window)
CREATE TABLE IF NOT EXISTS lecture_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    subject_name VARCHAR(200) NOT NULL,
    instructor_name VARCHAR(150),
    department_id INT REFERENCES academic_departments(id),
    geofence_lat DOUBLE PRECISION NOT NULL,
    geofence_lng DOUBLE PRECISION NOT NULL,
    geofence_radius INT DEFAULT 40,
    qr_secret VARCHAR(64) NOT NULL,
    status VARCHAR(20) DEFAULT 'closed' CHECK (status IN ('active', 'closed', 'cancelled')),
    starts_at TIMESTAMPTZ NOT NULL,
    ends_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- 5. Attendance Records (Atomic check-ins & anti-fraud proof)
CREATE TABLE IF NOT EXISTS attendance_records (
    id BIGSERIAL PRIMARY KEY,
    session_id UUID NOT NULL REFERENCES lecture_sessions(id) ON DELETE CASCADE,
    student_id UUID NOT NULL REFERENCES user_profiles(id) ON DELETE CASCADE,
    device_id VARCHAR(128) NOT NULL,
    check_in_time TIMESTAMPTZ DEFAULT NOW(),
    verification_mode VARCHAR(30) DEFAULT 'qr_dynamic_gps' CHECK (verification_mode IN ('qr_dynamic_gps', 'offline_proof', 'manual_override', 'absent')),
    accuracy_meters DOUBLE PRECISION,
    client_ip INET,
    CONSTRAINT unique_student_session UNIQUE (session_id, student_id)
);

-- 6. Security Audit Logs (Fraud telemetry & critical incident tracking)
CREATE TABLE IF NOT EXISTS security_audit_logs (
    id BIGSERIAL PRIMARY KEY,
    user_id UUID REFERENCES user_profiles(id),
    event_type VARCHAR(50) NOT NULL,
    severity VARCHAR(20) NOT NULL CHECK (severity IN ('LOW', 'MEDIUM', 'HIGH', 'CRITICAL')),
    details JSONB,
    ip_address INET,
    created_at TIMESTAMPTZ DEFAULT NOW()
);

-- Production Indexing Strategy
-- Fast filtering for ongoing active sessions
CREATE INDEX IF NOT EXISTS idx_sessions_active ON lecture_sessions (status) WHERE status = 'active';

-- High-concurrency attendance check-in lookup & uniqueness validation
CREATE INDEX IF NOT EXISTS idx_attendance_lookup ON attendance_records (session_id, student_id);

-- O(log N) student academic ID resolution during authentication
CREATE INDEX IF NOT EXISTS idx_profiles_academic_id ON user_profiles (academic_id);

-- Hardware device pinning validation
CREATE INDEX IF NOT EXISTS idx_devices_lookup ON user_devices (user_id, device_id);
