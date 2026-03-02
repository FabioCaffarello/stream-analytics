SHELL := /usr/bin/env bash

ODIN ?= odin
BUILD_DIR := build
SRC_NATIVE := src/platform/native
SRC_WEB := src/platform/web
WEB_DIR := web

WASM_OUT := $(BUILD_DIR)/app.wasm
NATIVE_OUT := $(BUILD_DIR)/app

COLLECTION_CORE := -collection:mr=src/core
COLLECTION_DEPS := -collection:deps=deps

ODIN_OPT ?= size
SERVE_PORT ?= 8080
NATIVE_WS_URL ?= ws://127.0.0.1:8080/ws
NATIVE_API_KEY ?= prod_key_1
NATIVE_FLAGS ?=

CORE_PKGS := app model ui widgets math util ports services

.PHONY: help build-native build-wasm build clean run run-native-compose run-native-offline serve check-wasm check-core check-core-imports check-widgets check-widgets-functional check-widgets-online

help:
	@echo "Client targets:"
	@echo "  make build-native       - build native binary  → build/app"
	@echo "  make build-wasm         - build wasm + copy    → build/app.wasm + web/app.wasm"
	@echo "  make build              - build both native and wasm"
	@echo "  make run                - build + run native   → opens GLFW window"
	@echo "  make run-native-compose - build + run native connected to local compose WS"
	@echo "  make run-native-offline - build + run native in offline/stub mode"
	@echo "  make clean              - remove build artifacts"
	@echo "  make serve              - build wasm + serve web/ on :$(SERVE_PORT)"
	@echo "  make check-wasm         - verify web/app.wasm serves HTTP 200"
	@echo "  make check-core         - odin check all core packages"
	@echo "  make check-core-imports - gate: fail if core imports platform or deps"
	@echo "  make check-widgets      - gate: offline soak asserts all widgets receive data"
	@echo "  make check-widgets-functional - alias of check-widgets (offline functional soak)"
	@echo "  make check-widgets-online - gate: online soak asserts all widgets receive live data"
	@echo ""
	@echo "Options:"
	@echo "  ODIN_OPT=size|speed|none  (default: size)"
	@echo "  SERVE_PORT=N             (default: 8080)"
	@echo "  NATIVE_WS_URL=ws://...   (default: $(NATIVE_WS_URL))"
	@echo "  NATIVE_API_KEY=...       (default: $(NATIVE_API_KEY))"
	@echo "  NATIVE_FLAGS='--sdl2'    (optional extra native flags)"
	@echo ""
	@echo "OLS profiles (edit ols.json to switch):"
	@echo "  native  - host OS/arch  (set checker_args: -collection:mr=src/core -collection:deps=deps)"
	@echo "  wasm    - js/wasm32     (set checker_args: -target:js_wasm32 -collection:mr=src/core)"

build-native:
	@mkdir -p $(BUILD_DIR)
	$(ODIN) build $(SRC_NATIVE) $(COLLECTION_CORE) $(COLLECTION_DEPS) -out:$(NATIVE_OUT)
	@echo "Built → $(NATIVE_OUT)"
	@ls -lh $(NATIVE_OUT)

build-wasm:
	@mkdir -p $(BUILD_DIR)
	$(ODIN) build $(SRC_WEB) -target:js_wasm32 $(COLLECTION_CORE) -out:$(WASM_OUT) -o:$(ODIN_OPT)
	@cp $(WASM_OUT) $(WEB_DIR)/app.wasm
	@echo "Built → $(WASM_OUT)"
	@echo "Copied → $(WEB_DIR)/app.wasm"
	@ls -lh $(WASM_OUT)

build: build-native build-wasm

run: build-native
	./$(NATIVE_OUT)

run-native-compose: build-native
	./$(NATIVE_OUT) --ws-url="$(NATIVE_WS_URL)" --api-key="$(NATIVE_API_KEY)" $(NATIVE_FLAGS)

run-native-offline: build-native
	./$(NATIVE_OUT) --offline $(NATIVE_FLAGS)

clean:
	rm -rf $(BUILD_DIR)
	rm -f $(WEB_DIR)/app.wasm

serve: build-wasm
	@echo "Serving $(WEB_DIR) on http://localhost:$(SERVE_PORT)"
	@echo "Open browser console to see Odin output"
	python3 -m http.server $(SERVE_PORT) --directory $(WEB_DIR)

check-wasm: build-wasm
	@python3 -m http.server $(SERVE_PORT) --directory $(WEB_DIR) &>/dev/null & \
	pid=$$!; \
	sleep 1; \
	status=$$(curl -s -o /dev/null -w '%{http_code}' http://localhost:$(SERVE_PORT)/app.wasm); \
	kill $$pid 2>/dev/null; \
	if [ "$$status" = "200" ]; then \
		echo "check-wasm: HTTP $$status OK — app.wasm served correctly"; \
	else \
		echo "check-wasm: HTTP $$status FAIL — app.wasm not served"; \
		exit 1; \
	fi

check-core:
	@for pkg in $(CORE_PKGS); do \
		echo "odin check core/$$pkg"; \
		$(ODIN) check src/core/$$pkg $(COLLECTION_CORE) -no-entry-point 2>&1 || exit 1; \
	done
	@echo "check-core: all packages OK"

check-core-imports:
	@./scripts/check-core-imports.sh

check-widgets:
	@./scripts/check-widgets-functional.sh

check-widgets-functional: check-widgets

check-widgets-online:
	@./scripts/check-widgets-online.sh
