BINARY := bin/lk

.PHONY: build run test vet clean

build:
	mkdir -p bin
	go build -o $(BINARY) .

run:
	nodemon -e go --exec "clear && go run main.go"

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
