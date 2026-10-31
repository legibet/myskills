.PHONY: build test lint install clean

build:
	go build -o myskills .

test:
	go test -race ./...

lint:
	go vet ./...
	golangci-lint run

install:
	go install .

clean:
	rm -f myskills
	rm -rf dist
