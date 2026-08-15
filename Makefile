BINARY := bin/lk
GOLANGCI_LINT ?= golangci-lint
LINT_TIMEOUT ?= 10m

.PHONY: build run test vet lint clean

build:
	mkdir -p bin
	go build -o $(BINARY) .

run:
	nodemon -e go --exec "clear && go run main.go"

test:
	go test ./...

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run --timeout $(LINT_TIMEOUT)

clean:
	rm -f $(BINARY)
