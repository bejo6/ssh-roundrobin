.PHONY: build clean install run test

build:
	go build -o bin/ssh-roundrobin ./cmd

clean:
	rm -rf bin/

install: clean
	go install ./cmd

run: build
	./bin/ssh-roundrobin

test:
	go test ./...

lint:
	golangci-lint run ./...

.DEFAULT_GOAL := build
