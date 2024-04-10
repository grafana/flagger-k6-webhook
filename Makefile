.PHONY: dev

dev:
	go build -o ./flagger-k6-webhook cmd/main.go
