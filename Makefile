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
	go run main.go -addr 95.142.102.175:993 -login verisure@rebel-x.org -password readsUre1\! -domotics-login golang -domotics-password tJEJqn8kaKFABHjrH9Jt -domotics-url https://217.62.16.236:8443

run-race: get
	go run -race

linux: get
	GOOS=linux GOARCH=amd64 go build -v -o ./verisure-imap -ldflags '-s -w --extldflags "-static" ' ./main.go

docker:
	docker build -t verisure:1.0 . -f Dockerfile

all: bench run
