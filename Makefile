# FRIDAY
#
# Two places this runs, and they are not the same thing:
#
#   local        this machine, `env = dev`, plain HTTP on the loopback,
#                started and stopped by hand, config.ini in the repo
#
#   production   the server, `env = production`, behind Caddy and TLS,
#                run by systemd and restarted on failure,
#                config.ini in /opt/friday
#
# Local targets have no prefix. Production targets all start with `prod-`
# and act over SSH.

BINARY   := friday
PKG      := ./cmd/server
CLIENT   := client

# FRIDAY's web UI is served from its own origin and fetches nothing from a
# CDN. Left to itself Flutter pulls CanvasKit from gstatic.com and Roboto
# from fonts.gstatic.com on every load: a request to Google on behalf of
# whoever opens FRIDAY, and a blank page on a network that cannot reach
# them. --no-web-resources-cdn bundles CanvasKit; the font is handled in
# the design tokens.
WEB_FLAGS := --no-web-resources-cdn
# Where the UI looks for the API. Needed in development only, because
# `flutter run` serves the UI from its own port and the page cannot infer
# where FRIDAY is. In production FRIDAY serves the UI, so the page's own
# origin is right and nothing is passed.
UI_SERVER ?= http://127.0.0.1:8080

# The Picovoice access key, for the wake word. Kept in a gitignored file
# rather than in the Makefile, the same way config.ini holds the server's
# credentials. Absent, the UI still builds and always-awake is simply not
# offered.
PICOVOICE_KEY := $(shell cat $(CLIENT)/.picovoice 2>/dev/null)

# What language to recognise speech as: how the owner actually speaks.
#
# This is the biggest lever on how accurately words are captured. A
# recogniser given the wrong accent model mishears steadily, and speaking
# more clearly does not fix it. Set here rather than left to follow the
# machine's locale, which reports en_CA and is nobody's accent.
FRIDAY_LANG ?= en-IN

# What FRIDAY answers to, always awake. One lower-case word: it is matched
# word by word against the transcript, so a name with a space in it would
# never match. Only "friday" has a list of the ways a recogniser commonly
# mishears it; another name gets the loose match alone.
#
#   make apk FRIDAY_NAME=jarvis
FRIDAY_NAME ?= friday

# What FRIDAY sounds like when it answers. A separate choice: what is
# spoken has to match the speaker or accuracy suffers, while the voice is
# only a preference.
FRIDAY_VOICE ?= en-US
BUILD    := build
PIDFILE  := .friday.pid
LOGFILE  := friday.log

# The server. Override on the command line if it moves:
#   make prod-status PROD_HOST=1.2.3.4
PROD_HOST ?= 136.111.221.56
PROD_USER ?= dhanush-12514
PROD_DIR  ?= /opt/friday
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
# `flutter run -d chrome` opens its own Chrome on a fresh temporary
# profile, and Chrome refuses speechSynthesis on a page that has had no
# user gesture. Asking by voice involves no click or keypress, so in
# development FRIDAY would never be allowed to speak — it fails silently,
# which looks exactly like the voice being broken. Served from FRIDAY in
# production the user has already clicked to log in, so this is a
# development flag only.
BROWSER_FLAGS := --web-browser-flag=--autoplay-policy=no-user-gesture-required

# Which port the development UI is served on, for `make ui`.
UI_PORT ?= 5100

ui: ## Serve the web UI for your own browser, with hot reload
	@echo "Open http://localhost:$(UI_PORT) in your usual Chrome."
	@echo "Not the one Flutter would launch itself: that runs on a"
	@echo "throwaway profile which produced no audio on this machine,"
	@echo "so FRIDAY appeared mute while reporting no error at all."
	@cd $(CLIENT) && flutter run -d web-server --web-port $(UI_PORT) \
		$(WEB_FLAGS) \
		--dart-define=FRIDAY_URL=$(UI_SERVER) \
		--dart-define=PICOVOICE_KEY=$(PICOVOICE_KEY) \
		--dart-define=FRIDAY_LANG=$(FRIDAY_LANG) \
		--dart-define=FRIDAY_VOICE=$(FRIDAY_VOICE) \
		--dart-define=FRIDAY_NAME=$(FRIDAY_NAME)

ui-chrome: ## Run the UI in a Chrome that Flutter launches itself
	@cd $(CLIENT) && flutter run -d chrome $(WEB_FLAGS) $(BROWSER_FLAGS) \
		--dart-define=FRIDAY_URL=$(UI_SERVER) \
		--dart-define=PICOVOICE_KEY=$(PICOVOICE_KEY) \
		--dart-define=FRIDAY_LANG=$(FRIDAY_LANG) \
		--dart-define=FRIDAY_VOICE=$(FRIDAY_VOICE) \
		--dart-define=FRIDAY_NAME=$(FRIDAY_NAME)

ui-build: ## Build the web UI the way it is deployed
	@cd $(CLIENT) && flutter build web $(WEB_FLAGS) \
		--dart-define=PICOVOICE_KEY=$(PICOVOICE_KEY) \
		--dart-define=FRIDAY_LANG=$(FRIDAY_LANG) \
		--dart-define=FRIDAY_VOICE=$(FRIDAY_VOICE) \
		--dart-define=FRIDAY_NAME=$(FRIDAY_NAME)

ui-check: ## Everything the client must pass before committing
	@cd $(CLIENT) && dart format --output=none --set-exit-if-changed lib test tool
	@cd $(CLIENT) && dart analyze
	@cd $(CLIENT) && flutter test

# Where a debug build looks for FRIDAY.
#
# The phone and this machine are on the same wifi, so a debug build talks to
# the server running here and a release build talks to the deployed one. That
# split is decided in server_url.dart by the build mode; all this does is tell
# a debug build which address to use, because the machine's own name means
# nothing on the phone.
#
# Detected rather than configured: the address changes with the network, and a
# stale one in a file is a confusing way to fail. Override it for a server
# somewhere else:
#
#   make apk PHONE_SERVER=http://192.168.1.20:8080
LAN_IP := $(shell ip -4 route get 1.1.1.1 2>/dev/null | \
	awk '{for (i = 1; i <= NF; i++) if ($$i == "src") print $$(i + 1)}')
PHONE_SERVER ?= $(if $(LAN_IP),http://$(LAN_IP):8080,)

# Passed to debug builds only. A release build ignores FRIDAY_DEV_URL, so an
# installed app cannot end up pointed at a laptop that is not on the network.
DEV_URL_FLAG = $(if $(PHONE_SERVER),--dart-define=FRIDAY_DEV_URL=$(PHONE_SERVER),)

# Which Whisper weights the phone listens with. base.en keeps up with
# speech; small.en hears an accent better but decoded slower than talking on
# the phone it was tried on, so the words arrived late or not at all. Worth
# retrying on faster hardware:
#
#   make apk WHISPER_MODEL=small.en
WHISPER_MODEL ?= base.en

# Which recogniser Android listens with: native or whisper.
#
#   make apk EARS=native     Android's own SpeechRecognizer
#   make apk EARS=sherpa     a streaming transducer, on the device
#   make apk EARS=whisper    Whisper on the device, WHISPER_MODEL picks the size
#
# native by default. Whisper's accuracy argument only holds at small.en and
# up, which decoded slower than speech on the phone it was tried on; base.en
# keeps up but is a weak model against one trained for en-IN.
EARS ?= native
WHISPER_FLAG = --dart-define=FRIDAY_EARS=$(EARS) \
	--dart-define=FRIDAY_WHISPER_MODEL=$(WHISPER_MODEL)

# Which processor architecture to build for. Whisper is compiled from C once
# per architecture and each takes minutes, so an APK for one known phone
# builds one. Every Android phone since about 2016 is arm64-v8a; an older or
# emulated one needs `make apk ABI=armeabi-v7a` or `ABI=x86_64`. Empty
# builds all of them, which is what a store upload would want.
ABI ?= arm64-v8a
ABI_FLAGS = $(if $(ABI),--target-platform $(ABI_PLATFORM) -Pfriday.abi=$(ABI),)
ABI_PLATFORM = $(strip \
	$(if $(filter arm64-v8a,$(ABI)),android-arm64,) \
	$(if $(filter armeabi-v7a,$(ABI)),android-arm,) \
	$(if $(filter x86_64,$(ABI)),android-x64,))

apk: ## Build a debug APK to sideload onto a phone
	@cd $(CLIENT) && flutter build apk --debug $(ABI_FLAGS) $(DEV_URL_FLAG) $(WHISPER_FLAG) \
		--dart-define=FRIDAY_LANG=$(FRIDAY_LANG) \
		--dart-define=FRIDAY_VOICE=$(FRIDAY_VOICE) \
		--dart-define=FRIDAY_NAME=$(FRIDAY_NAME)
	@echo "APK: $(CLIENT)/build/app/outputs/flutter-apk/app-debug.apk"

apk-release: ## Build a release APK
	@cd $(CLIENT) && flutter build apk --release $(ABI_FLAGS) $(WHISPER_FLAG) \
		--dart-define=FRIDAY_LANG=$(FRIDAY_LANG) \
		--dart-define=FRIDAY_VOICE=$(FRIDAY_VOICE) \
		--dart-define=FRIDAY_NAME=$(FRIDAY_NAME)
	@echo "APK: $(CLIENT)/build/app/outputs/flutter-apk/app-release.apk"

phone: ## Run on a connected phone, with hot reload
	@cd $(CLIENT) && flutter run -d android $(DEV_URL_FLAG) $(WHISPER_FLAG) \
		--dart-define=FRIDAY_LANG=$(FRIDAY_LANG) \
		--dart-define=FRIDAY_VOICE=$(FRIDAY_VOICE) \
		--dart-define=FRIDAY_NAME=$(FRIDAY_NAME)

createuser: ## Create a user locally (prompts for a password)
	@go run $(PKG) createuser $(USER_NAME)

.PHONY: ui ui-chrome ui-build ui-check apk apk-release phone createuser

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
	@scp -q $(BUILD)/$(BINARY) $(PROD_USER)@$(PROD_HOST):/tmp/friday.new
	@$(SSH) 'sudo systemctl stop friday \
		&& sudo mv /tmp/friday.new $(PROD_DIR)/friday \
		&& sudo chown friday:friday $(PROD_DIR)/friday \
		&& sudo chmod 750 $(PROD_DIR)/friday \
		&& sudo systemctl start friday'
	@sleep 4
	@$(MAKE) --no-print-directory prod-status

.PHONY: prod-start
prod-start: ## Start it on the server
	@$(SSH) 'sudo systemctl start friday' && echo "started"
	@sleep 3
	@$(MAKE) --no-print-directory prod-status

.PHONY: prod-stop
prod-stop: ## Stop it on the server (drains first)
	@$(SSH) 'sudo systemctl stop friday' && echo "stopped"

.PHONY: prod-restart
prod-restart: ## Restart it on the server
	@$(SSH) 'sudo systemctl restart friday' && echo "restarted"
	@sleep 3
	@$(MAKE) --no-print-directory prod-status

.PHONY: prod-status
prod-status: ## What the server is doing
	@$(SSH) 'printf "  friday: %s   mysql: %s   caddy: %s\n" \
		"$$(systemctl is-active friday)" "$$(systemctl is-active mysql)" "$$(systemctl is-active caddy)"; \
		free -m | awk "/Mem:/ {printf \"  memory: %s MB used, %s MB free\\n\", \$$3, \$$7}"' 2>/dev/null || echo "  unreachable"
	@printf "  %s/health -> " "$(PROD_URL)"
	@curl -s -m 10 -o /dev/null -w "%{http_code}\n" $(PROD_URL)/health 2>/dev/null || echo "no answer"
	@curl -s -m 10 $(PROD_URL)/ready 2>/dev/null | sed 's/^/  ready: /' || true

.PHONY: prod-logs
prod-logs: ## Follow the server's log
	@$(SSH) -t 'sudo journalctl -u friday -f -n 40'

.PHONY: prod-ssh
prod-ssh: ## Open a shell on the server
	@$(SSH) -t

.PHONY: prod-createuser
prod-createuser: ## Create a user on the server (prompts for a password)
	@$(SSH) -t 'sudo -u friday $(PROD_DIR)/friday createuser $(USER_NAME)'

.PHONY: prod-backup
prod-backup: ## Run a database backup on the server now
	@$(SSH) 'sudo systemctl start friday-backup.service && sleep 5 && sudo ls -lh /var/backups/friday/ | tail -3'
