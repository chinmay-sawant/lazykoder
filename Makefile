BINARY := bin/lk

.PHONY: build run test vet clean

build:
	mkdir -p bin
	go build -o $(BINARY) .

run: build
	./$(BINARY)

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BINARY)
