.PHONY: dev build push push-latest
VERSION := $(shell git describe --tags --dirty --always)
IMAGE ?= jduchesnegrafana/flagger-k6-webhook

dev:
	go build -o flagger-k6-webhooks

build:
	docker build -t $(IMAGE) .
	docker tag $(IMAGE) $(IMAGE):$(VERSION)

push: build
	docker push $(IMAGE):$(VERSION)

push-latest: push
	docker push $(IMAGE)

