.PHONY: build run dev clean

build:
	go build -o hub ./cmd/hub

run: build
	./hub

dev:
	go run ./cmd/hub

clean:
	rm -f hub
	rm -rf data/
