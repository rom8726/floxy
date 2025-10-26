_COMPOSE=docker compose -f dev/docker-compose.yml --project-name ${NAMESPACE}

dev-up: ## Up the environment in docker compose
	${_COMPOSE} --profile env up -d

dev-down: ## Down the environment in docker compose
	${_COMPOSE} --profile env down --remove-orphans

dev-clean: ## Down the environment in docker compose with images cleanup
	${_COMPOSE} --profile env down --remove-orphans -v --rmi all

dev-restart: dev-down dev-up ## Restart the environment in docker compose
