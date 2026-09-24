# FRIDAY
#
# Two places this runs, and they are not the same thing:
#
#   local        this machine, `env = dev`, plain HTTP on the loopback,
#                started and stopped by hand, config.ini in the repo
#
#   production   the server, `env = production`, behind Caddy and TLS,
#                run by systemd and restarted on failure,
#                config.ini in /opt/personal-assistant
#
# Local targets have no prefix. Production targets all start with `prod-`
# and act over SSH.

BINARY   := personal-assistant
PKG      := ./cmd/server

BUILD    := build
PIDFILE  := .personal-assistant.pid
LOGFILE  := personal-assistant.log

# The server. Override on the command line if it moves:
#   make prod-status PROD_HOST=1.2.3.4
PROD_HOST ?= 136.111.221.56
PROD_USER ?= dhanush-12514
# What the service, its unit, its directory and its OS user are called on the
# deployed machine. Renaming these on a running server is a migration, not an
# edit: see deployments/README.md.
PROD_NAME ?= personal-assistant
PROD_DIR  ?= /opt/$(PROD_NAME)
PROD_URL  ?= https://friday-server.duckdns.org
SSH       := ssh -o ConnectTimeout=20 $(PROD_USER)@$(PROD_HOST)

.DEFAULT_GOAL := help

# ############################# Help ######################################## #

.PHONY: help
help: ## List the targets
	@echo "local:"
	@grep -hE '^[a-z][a-z-]*:.*##' $(MAKEFILE_LIST) | grep -v '^prod-' | sort | awk -F':.*##' '{printf "  %-16s %s\n", $$1, $$2}'
	@echo
	@echo "production ($(PROD_HOST)):"
	@grep -hE '^prod-[a-z-]*:.*##' $(MAKEFILE_LIST) | sort | awk -F':.*##' '{printf "  %-16s %s\n", $$1, $$2}'

# ############################# Local ####################################### #

.PHONY: build
build: ## Build for this machine
	@go build -o $(BINARY) $(PKG)
	@echo "built ./$(BINARY)"

.PHONY: run
run: ## Run in the foreground, logs to the terminal (ctrl-c to stop)
	@go run $(PKG)

.PHONY: start
start: build ## Start in the background
	@if [ -f $(PIDFILE) ] && kill -0 $$(cat $(PIDFILE)) 2>/dev/null; then \
		echo "already running (pid $$(cat $(PIDFILE)))"; exit 0; \
	fi
	@./$(BINARY) > $(LOGFILE) 2>&1 & echo $$! > $(PIDFILE)
	@sleep 2
	@if kill -0 $$(cat $(PIDFILE)) 2>/dev/null; then \
		echo "started (pid $$(cat $(PIDFILE))), logging to $(LOGFILE)"; \
		grep -o 'addr=[^ ]*' $(LOGFILE) | tail -1 | sed 's/^/  listening on /'; \
	else \
		rm -f $(PIDFILE); echo "failed to start:"; tail -3 $(LOGFILE); exit 1; \
	fi

.PHONY: stop
stop: ## Stop the background one
	@if [ -f $(PIDFILE) ] && kill -0 $$(cat $(PIDFILE)) 2>/dev/null; then \
		kill -TERM $$(cat $(PIDFILE)); \
		for i in 1 2 3 4 5 6 7 8 9 10; do kill -0 $$(cat $(PIDFILE)) 2>/dev/null || break; sleep 1; done; \
		rm -f $(PIDFILE); echo "stopped"; \
	else \
		rm -f $(PIDFILE); echo "not running"; \
	fi
	@# Anything else of ours left holding the port, from a crash or an older run.
	@pkill -x $(BINARY) 2>/dev/null && echo "  (also cleared a stray $(BINARY))" || true

.PHONY: restart
restart: stop start ## Stop then start

.PHONY: status
status: ## Is it running, and is it answering
	@if [ -f $(PIDFILE) ] && kill -0 $$(cat $(PIDFILE)) 2>/dev/null; then \
		echo "running (pid $$(cat $(PIDFILE)))"; \
	else \
		echo "not running"; \
	fi
	@ss -lntp 2>/dev/null | grep ':8080' | awk '{print "  listening " $$4}' || echo "  nothing on :8080"
	@curl -s -m 3 localhost:8080/health 2>/dev/null | sed 's/^/  health: /' || echo "  health: no answer"
	@curl -s -m 3 localhost:8080/ready  2>/dev/null | sed 's/^/  ready:  /' || true

.PHONY: logs
logs: ## Follow the local log
	@tail -f $(LOGFILE)

.PHONY: test
test: ## Run the tests
	@go test ./... -race

.PHONY: check
check: ## Everything that must pass before committing
	@gofmt -l .
	@go vet ./...
	@go test ./... -race

.PHONY: clean
createuser: ## Create a user locally (prompts for a password)
	@go run $(PKG) createuser $(USER_NAME)

.PHONY: createuser

clean: stop ## Stop and remove build output
	@rm -rf $(BUILD) $(BINARY) $(LOGFILE) $(PIDFILE)
	@echo "cleaned"

# ############################# Production ################################## #

.PHONY: deploy-build
deploy-build: ## Build the binary the server runs (linux/amd64)
	@mkdir -p $(BUILD)
	@GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o $(BUILD)/$(BINARY) $(PKG)
	@echo "$(BUILD)/$(BINARY): $$(du -h $(BUILD)/$(BINARY) | cut -f1)"

.PHONY: prod-deploy
prod-deploy: check deploy-build ## Test, build, upload and restart the server
	@scp -q $(BUILD)/$(BINARY) $(PROD_USER)@$(PROD_HOST):/tmp/$(PROD_NAME).new
	@$(SSH) 'sudo systemctl stop $(PROD_NAME) \
		&& sudo mv /tmp/$(PROD_NAME).new $(PROD_DIR)/$(PROD_NAME) \
		&& sudo chown assistant:assistant $(PROD_DIR)/$(PROD_NAME) \
		&& sudo chmod 750 $(PROD_DIR)/$(PROD_NAME) \
		&& sudo systemctl start $(PROD_NAME)'
	@sleep 4
	@$(MAKE) --no-print-directory prod-status

.PHONY: prod-start
prod-start: ## Start it on the server
	@$(SSH) 'sudo systemctl start $(PROD_NAME)' && echo "started"
	@sleep 3
	@$(MAKE) --no-print-directory prod-status

.PHONY: prod-stop
prod-stop: ## Stop it on the server (drains first)
	@$(SSH) 'sudo systemctl stop $(PROD_NAME)' && echo "stopped"

.PHONY: prod-restart
prod-restart: ## Restart it on the server
	@$(SSH) 'sudo systemctl restart $(PROD_NAME)' && echo "restarted"
	@sleep 3
	@$(MAKE) --no-print-directory prod-status

.PHONY: prod-status
prod-status: ## What the server is doing
	@$(SSH) 'printf "  $(PROD_NAME): %s   mysql: %s   caddy: %s\n" \
		"$$(systemctl is-active $(PROD_NAME))" "$$(systemctl is-active mysql)" "$$(systemctl is-active caddy)"; \
		free -m | awk "/Mem:/ {printf \"  memory: %s MB used, %s MB free\\n\", \$$3, \$$7}"' 2>/dev/null || echo "  unreachable"
	@printf "  %s/health -> " "$(PROD_URL)"
	@curl -s -m 10 -o /dev/null -w "%{http_code}\n" $(PROD_URL)/health 2>/dev/null || echo "no answer"
	@curl -s -m 10 $(PROD_URL)/ready 2>/dev/null | sed 's/^/  ready: /' || true

.PHONY: prod-logs
prod-logs: ## Follow the server's log
	@$(SSH) -t 'sudo journalctl -u $(PROD_NAME) -f -n 40'

.PHONY: prod-ssh
prod-ssh: ## Open a shell on the server
	@$(SSH) -t

.PHONY: prod-createuser
prod-createuser: ## Create a user on the server (prompts for a password)
	@$(SSH) -t 'sudo -u assistant $(PROD_DIR)/$(PROD_NAME) createuser $(USER_NAME)'

.PHONY: prod-backup
prod-backup: ## Run a database backup on the server now
	@$(SSH) 'sudo systemctl start $(PROD_NAME)-backup.service && sleep 5 && sudo ls -lh /var/backups/$(PROD_NAME)/ | tail -3'
