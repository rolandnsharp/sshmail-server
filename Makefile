.PHONY: build tui run dev clean

build:
	go build -o hub ./cmd/hub

tui:
	go build -o sshmail ./cmd/tui

run: build
	./hub

dev:
	go run ./cmd/hub

clean:
	rm -f hub sshmail
	rm -rf data/
