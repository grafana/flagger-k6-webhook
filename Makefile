.PHONY: dev build push push-latest
VERSION := $(shell git describe --tags --dirty --always)-10
IMAGE ?= ghcr.io/grafana/flagger-k6-webhook

dev:
	go build -o flagger-k6-webhooks

build:
	docker build -t $(IMAGE) .
	docker tag $(IMAGE) $(IMAGE):$(VERSION)

push: build
	docker push $(IMAGE):$(VERSION)

push-latest: build
	docker push $(IMAGE)

drone:
	rm -f .drone/drone.yml
	drone jsonnet --stream --format --source .drone/drone.jsonnet --target .drone/temp.yml
	drone lint .drone/temp.yml --trusted
	drone sign --save grafana/flagger-k6-webhook .drone/temp.yml
	mv .drone/temp.yml .drone/drone.yml