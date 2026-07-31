.PHONY: setup test

setup:
	git config core.hooksPath .githooks
	chmod +x .githooks/pre-push
	@echo "Git hooks installed."

test:
	go test ./...
