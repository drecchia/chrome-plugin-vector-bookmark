.PHONY: build install uninstall clean tidy test run

BIN := bin/vbmd

build:
	@mkdir -p bin
	go build -o $(BIN) ./cmd/vbmd/

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
	rm -f ~/.local/bin/vbmd
	rm -f ~/.config/google-chrome/NativeMessagingHosts/com.vbm.daemon.json
	rm -f ~/.config/chromium/NativeMessagingHosts/com.vbm.daemon.json
	systemctl --user stop vbmd 2>/dev/null || true
	systemctl --user disable vbmd 2>/dev/null || true
	rm -f ~/.config/systemd/user/vbmd.service
	systemctl --user daemon-reload
	rm -f ~/.local/share/vbm/session.json
	@echo "Done. vbm data in ~/.local/share/vbm/ was NOT removed."

clean:
	rm -rf bin/
