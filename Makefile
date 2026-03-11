VPS_HOST := root@43.229.61.163
VPS_SSH  := ssh -p 2200 $(VPS_HOST)

.PHONY: build tui run dev deploy clean

build:
	go build -o hub ./cmd/hub

tui:
	go build -o sshmail ./cmd/tui

run: build
	./hub

dev:
	go run ./cmd/hub

deploy:
	GOOS=linux GOARCH=amd64 go build -o hub-linux ./cmd/hub
	scp -P 2200 hub-linux $(VPS_HOST):/tmp/sshmail-hub-new
	$(VPS_SSH) 'systemctl stop sshmail && cp /tmp/sshmail-hub-new /usr/local/bin/sshmail && chmod +x /usr/local/bin/sshmail && systemctl start sshmail'
	@echo "Deployed. Verifying..."
	@sleep 2
	$(VPS_SSH) 'systemctl status sshmail --no-pager | head -5'
	rm -f hub-linux

clean:
	rm -f hub hub-linux sshmail
	rm -rf data/
