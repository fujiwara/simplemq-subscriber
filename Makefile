.PHONY: clean test

simplemq-subscriber: go.* *.go
	go build -o $@ ./cmd/simplemq-subscriber

clean:
	rm -rf simplemq-subscriber dist/

test:
	go test -v ./...

install:
	go install github.com/fujiwara/simplemq-subscriber/cmd/simplemq-subscriber

dist:
	goreleaser build --snapshot --clean
