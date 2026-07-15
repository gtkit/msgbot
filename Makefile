.PHONY: tool check tag release-check-root


LINT_TARGETS ?= ./...
MIN_PROVIDER_COVERAGE ?= 80.0
REMOTE ?= gtkit

tool: ## Lint Go code with the installed golangci-lint
	@ echo "▶️ golangci-lint run"
	golangci-lint run $(LINT_TARGETS)
	gofumpt -l -w .
	@ echo "✅ golangci-lint run"

## govulncheck 检查漏洞 go install golang.org/x/vuln/cmd/govulncheck@latest
check:
	govulncheck $(LINT_TARGETS)

release-check-root: ## Run release checks for root module and enforce provider coverage threshold
	go vet ./...
	golangci-lint run ./...
	go test -race -count=1 -timeout=5m ./...
	go test -run '^$$' -bench=. -benchmem -count=3 ./...
	go test -coverprofile=coverage.out ./provider
	@cover=$$(go tool cover -func=coverage.out | awk '/^total:/ {gsub("%","",$$3); print $$3}'); \
	awk -v got="$$cover" -v min="$(MIN_PROVIDER_COVERAGE)" 'BEGIN { if (got+0 < min+0) exit 1 }' || \
		{ echo "provider coverage $$cover% is below $(MIN_PROVIDER_COVERAGE)%"; exit 1; }; \
	echo "provider coverage $$cover% (threshold $(MIN_PROVIDER_COVERAGE)%)"


## 推送标签到远程仓库时，通常不需要指定分支
tag: ## Create a local annotated tag. Usage: make tag VERSION=v1.5.0 MESSAGE='- fix: 修复问题'
	@test -n "$(VERSION)" || { echo "VERSION is required, for example: make tag VERSION=v1.5.0 MESSAGE='- fix: 修复问题'"; exit 1; }
	@test -n "$(MESSAGE)" || { echo "MESSAGE is required, for example: MESSAGE='- fix: 修复问题'"; exit 1; }
	@printf '%s\n' "$(VERSION)" | grep -Eq '^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$$' || { echo "invalid VERSION: $(VERSION)"; exit 1; }
	@git rev-parse -q --verify "refs/tags/$(VERSION)" >/dev/null && { echo "tag $(VERSION) already exists"; exit 1; } || true
	git tag -a "$(VERSION)" -m "版本 $(VERSION)" -m "主要变更：" -m "$(MESSAGE)"
	@echo "created local tag $(VERSION). Push manually with: git push $(REMOTE) $(VERSION)"
