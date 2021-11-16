.PHONY: dev build push push-latest
VERSION := $(shell git describe --tags --dirty --always)-10
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

drone:
	rm -f .drone/drone.yml
	drone jsonnet --stream --format --source .drone/drone.jsonnet --target .drone/drone.yml
	drone lint .drone/drone.yml
	drone sign --save grafana/flagger-k6-webhook .drone/drone.yml