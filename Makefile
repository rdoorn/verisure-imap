.PHONY: all test bench

test-v:
	go test ./... -v  -timeout=60000ms

test:
	go test ./...
	go test ./... -short -race
	go vet

bench: test
	go test ./... -test.run=NONE -test.bench=. -test.benchmem

get:
	go get

run: get
    go run main.go -addr "rebel-x.org:993" -login "verisure@rebel-x.org" -password "readsUre1\!"

run-race: get
	go run -race

all: bench run
