BIN_GOLANGCI_LINT = tools/golangci-lint
$(BIN_GOLANGCI_LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b tools v2.6.2

.PHONY: lint test

lint: $(BIN_GOLANGCI_LINT)
	$(BIN_GOLANGCI_LINT) run ./...

test:
	go test ./...
