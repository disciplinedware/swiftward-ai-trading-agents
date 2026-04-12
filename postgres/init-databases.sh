#!/bin/bash
set -e

# Create application databases and users.
# Runs once on first container start via /docker-entrypoint-initdb.d/.

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$POSTGRES_DB" <<-EOSQL
    CREATE USER swiftward WITH PASSWORD 'swiftward';
    CREATE DATABASE swiftward OWNER swiftward;

    CREATE USER trading WITH PASSWORD 'trading';
    CREATE DATABASE trading OWNER trading;

    CREATE USER py_trading WITH PASSWORD 'py_trading';
    CREATE DATABASE py_trading OWNER py_trading;

    CREATE USER solid_loop_trading WITH PASSWORD 'solid_loop_trading';
    CREATE DATABASE solid_loop_trading_production OWNER solid_loop_trading;
    CREATE DATABASE solid_loop_trading_production_cache OWNER solid_loop_trading;
    CREATE DATABASE solid_loop_trading_production_cable OWNER solid_loop_trading;
EOSQL
