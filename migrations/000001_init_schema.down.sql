-- Phase 1: Revert Core Database Schema Migration for Hader-V2

-- Drop indexes in reverse
DROP INDEX IF EXISTS idx_devices_lookup;
DROP INDEX IF EXISTS idx_profiles_academic_id;
DROP INDEX IF EXISTS idx_attendance_lookup;
DROP INDEX IF EXISTS idx_sessions_active;

-- Drop tables in reverse order of foreign key dependencies
DROP TABLE IF EXISTS security_audit_logs CASCADE;
DROP TABLE IF EXISTS attendance_records CASCADE;
DROP TABLE IF EXISTS lecture_sessions CASCADE;
DROP TABLE IF EXISTS user_devices CASCADE;
DROP TABLE IF EXISTS user_profiles CASCADE;
DROP TABLE IF EXISTS academic_departments CASCADE;
