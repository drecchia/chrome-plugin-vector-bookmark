.PHONY: build build-windows build-all install uninstall clean tidy test run docker-build docker-up docker-down docker-logs

BIN     := bin/vbmd
BIN_WIN := bin/vbmd.exe

build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/vbmd/

build-windows:
	@mkdir -p bin
	GOOS=windows GOARCH=amd64 go build -o $(BIN_WIN) ./cmd/vbmd/

build-all: build build-windows

tidy:
	go mod tidy

test:
	go test ./...

run: build
	./$(BIN) server

install: build
	@bash install/install.sh $(PWD)/$(BIN)

uninstall:
	@echo "Removing vbmd..."
	systemctl --user stop vbmd 2>/dev/null || true
	systemctl --user disable vbmd 2>/dev/null || true
	rm -f ~/.config/systemd/user/vbmd.service
	systemctl --user daemon-reload 2>/dev/null || true
	rm -f ~/.local/bin/vbmd
	@echo "Done. Data in ~/.local/share/vbm/ was NOT removed."

clean:
	rm -rf bin/

# ── Docker targets (run from repo root) ───────────────────────────────────────
docker-build:
	docker compose -f ../docker-compose.yml build

docker-up:
	docker compose -f ../docker-compose.yml up -d
	@echo "Daemon running in Docker. Logs: make docker-logs"

docker-down:
	docker compose -f ../docker-compose.yml down

docker-logs:
	docker compose -f ../docker-compose.yml logs -f vbmd
