#!/bin/bash
# Source: https://github.com/vsokolovsky555/docker-postgresql-multiple-databases/blob/master/init.sh

set -e
set -u

function create_user_and_database() {
	local database=$1
	echo "  Creating user and database '$database'"
	psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
	    CREATE DATABASE $database;
EOSQL
}

function show_created_user_and_database() {
	echo "  Show databases"
	psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --list
	echo "  Show users"
	psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" <<-EOSQL
			SELECT u.usename AS "Role name",
				CASE WHEN u.usesuper AND u.usecreatedb THEN CAST('superuser, create
			database' AS pg_catalog.text)
						WHEN u.usesuper THEN CAST('superuser' AS pg_catalog.text)
						WHEN u.usecreatedb THEN CAST('create database' AS
			pg_catalog.text)
						ELSE CAST('' AS pg_catalog.text)
				END AS "Attributes"
			FROM pg_catalog.pg_user u
			ORDER BY 1;
EOSQL
}

if [ -n "$POSTGRES_MULTIPLE_DATABASES" ]; then
	echo "Multiple database creation requested: $POSTGRES_MULTIPLE_DATABASES"
	for db in $(echo $POSTGRES_MULTIPLE_DATABASES | tr ',' ' '); do
		create_user_and_database $db
	done
	echo "Multiple databases created"
	show_created_user_and_database
	echo "  Finish work ------------------------------------------------------------------"
fi
