#!/bin/bash

set -e
set -u

function create_extension_in_database() {
	local database=$1
	echo "  Creating extensions for database '$database'"
	psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname="$database" <<-EOSQL
      CREATE EXTENSION IF NOT EXISTS "pg_stat_statements";
      CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
EOSQL
}

if [ -n "$POSTGRES_MULTIPLE_DATABASES" ]; then
	echo "Multiple database install extension: $POSTGRES_MULTIPLE_DATABASES"
	for db in $(echo $POSTGRES_MULTIPLE_DATABASES | tr ',' ' '); do
		create_extension_in_database $db
	done
	echo "Multiple database installed extension"
	show_created_user_and_database
	echo "  Finish work ------------------------------------------------------------------"
fi
