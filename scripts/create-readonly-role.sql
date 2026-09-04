-- Run as a PostgreSQL administrator after replacing the password placeholder.
-- The application additionally opens every transaction as READ ONLY.
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'cores_mcp_readonly') THEN
        CREATE ROLE cores_mcp_readonly NOLOGIN;
    END IF;
END $$;

GRANT CONNECT ON DATABASE rentalcore TO cores_mcp_readonly;
GRANT USAGE ON SCHEMA public TO cores_mcp_readonly;
GRANT SELECT ON ALL TABLES IN SCHEMA public TO cores_mcp_readonly;
GRANT SELECT ON ALL SEQUENCES IN SCHEMA public TO cores_mcp_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON TABLES TO cores_mcp_readonly;
ALTER DEFAULT PRIVILEGES IN SCHEMA public GRANT SELECT ON SEQUENCES TO cores_mcp_readonly;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'cores_mcp') THEN
        CREATE ROLE cores_mcp LOGIN PASSWORD 'REPLACE_WITH_A_RANDOM_PASSWORD';
    END IF;
END $$;

GRANT cores_mcp_readonly TO cores_mcp;
ALTER ROLE cores_mcp SET default_transaction_read_only = on;
ALTER ROLE cores_mcp SET statement_timeout = '8s';
