_COMPOSE=docker compose -f dev/docker-compose.yml --project-name ${NAMESPACE}

dev-up: ## Поднять окружение в docker compose
	${_COMPOSE} --profile env up -d

dev-down: ## Погасить окружение в docker compose
	${_COMPOSE} --profile env down --remove-orphans

dev-clean: ## Погасить окружение в docker compose с очисткой образов
	${_COMPOSE} --profile env down --remove-orphans -v --rmi all

dev-restart: dev-down dev-up ## Перезапустить окружение в docker compose
