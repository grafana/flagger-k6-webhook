.PHONY: dev build push push-latest
VERSION := $(shell git describe --tags --dirty --always)
IMAGE ?= ghcr.io/grafana/flagger-k6-webhook

dev:
	go build -o ./flagger-k6-webhook cmd/main.go

build:
	docker build -t $(IMAGE) .
	docker tag $(IMAGE) $(IMAGE):$(VERSION)

push: build
	docker push $(IMAGE):$(VERSION)

push-latest: build
	docker push $(IMAGE)
