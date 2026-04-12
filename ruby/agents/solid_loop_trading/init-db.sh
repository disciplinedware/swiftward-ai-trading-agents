#!/bin/bash
set -e

# The official postgres image creates POSTGRES_DB automatically.
# This script creates additional databases for Solid Cache and Solid Cable.

echo "Creating additional databases for Solid Cache and Solid Cable..."

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "postgres" <<-EOSQL
	CREATE DATABASE solid_loop_trading_production_cache;
	CREATE DATABASE solid_loop_trading_production_cable;
	GRANT ALL PRIVILEGES ON DATABASE solid_loop_trading_production_cache TO "$POSTGRES_USER";
	GRANT ALL PRIVILEGES ON DATABASE solid_loop_trading_production_cable TO "$POSTGRES_USER";
EOSQL

