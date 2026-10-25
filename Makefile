build:
	go build -o burnrate .

test:
	go vet ./...
	go test ./...

install: build
	./burnrate install

uninstall:
	./burnrate uninstall

.PHONY: build test install uninstall
