GO_SOURCES=$(shell find . -name "*.go")

BIN_GOLANGCI_LINT = tools/golangci-lint
$(BIN_GOLANGCI_LINT):
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b tools v2.6.2

.PHONY: lint test

lint: $(BIN_GOLANGCI_LINT)
	$(BIN_GOLANGCI_LINT) run ./...

test:
	go test -v -count=1 ./...

ex0001.cmd: $(GO_SOURCES)
	go build -o ex0001.cmd internal/cmd/ex0001/*.go

ex0002.cmd: $(GO_SOURCES)
	go build -o ex0002.cmd internal/cmd/ex0002/*.go
