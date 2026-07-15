## ====================================================================
## AI Service Reporter Plugin — Makefile
##
## ⚠️ 이 Makefile 은 Unix 쉘 (bash / zsh / git-bash / WSL) 이 필요합니다.
##    Windows cmd / PowerShell 에서 GNU make 만 깔아 돌리면 mkdir -p 등이
##    실패합니다. Windows 에서는 build.ps1 을 쓰거나 WSL/git-bash 를 사용.
## ====================================================================

PLUGIN_ID  ?= ai_service_reporter

.PHONY: all
all: dist

## ------- server (멀티 플랫폼 cross-compile) -------
.PHONY: server
server:
	@mkdir -p server/dist
	cd server && env GOOS=linux  GOARCH=amd64 go build -o dist/plugin-linux-amd64
	cd server && env GOOS=linux  GOARCH=arm64 go build -o dist/plugin-linux-arm64
	cd server && env GOOS=darwin GOARCH=amd64 go build -o dist/plugin-darwin-amd64
	cd server && env GOOS=darwin GOARCH=arm64 go build -o dist/plugin-darwin-arm64
	cd server && env GOOS=windows GOARCH=amd64 go build -o dist/plugin-windows-amd64.exe

## ------- webapp -------
.PHONY: webapp
webapp:
	cd webapp && npm install --no-audit --no-fund
	cd webapp && npm run build
	@test -f webapp/dist/main.js || (echo "❌ webapp/dist/main.js not produced — webpack failed" && exit 1)
	@echo "✅ webapp/dist/main.js OK"

## ------- bundle -------
.PHONY: dist
dist: server webapp
	rm -rf dist
	mkdir -p dist/$(PLUGIN_ID)
	cp plugin.json   dist/$(PLUGIN_ID)/
	cp -r assets     dist/$(PLUGIN_ID)/
	mkdir -p dist/$(PLUGIN_ID)/server
	cp -r server/dist dist/$(PLUGIN_ID)/server/
	mkdir -p dist/$(PLUGIN_ID)/webapp
	cp -r webapp/dist dist/$(PLUGIN_ID)/webapp/
	cd dist && tar -czf $(PLUGIN_ID).tar.gz $(PLUGIN_ID)
	@echo "✅ Bundle ready → dist/$(PLUGIN_ID).tar.gz"

## ------- 개별 빌드만 (디버깅용) -------
.PHONY: server-linux
server-linux:
	@mkdir -p server/dist
	cd server && env GOOS=linux GOARCH=amd64 go build -o dist/plugin-linux-amd64
	@echo "✅ server/dist/plugin-linux-amd64 OK"

.PHONY: clean
clean:
	rm -rf dist server/dist webapp/dist webapp/node_modules
