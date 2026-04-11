FROM golang:1.26.2-alpine@sha256:c2a1f7b2095d046ae14b286b18413a05bb82c9bca9b25fe7ff5efef0f0826166 AS build

ARG GO_LDFLAGS

RUN mkdir /app
WORKDIR /app
COPY . /app/
RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux go build -ldflags "${GO_LDFLAGS} -extldflags '-static'" -o /app/flagger-k6-webhook cmd/main.go

FROM alpine:3.23@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

COPY --from=build /app/flagger-k6-webhook /usr/bin/flagger-k6-webhook
COPY --from=grafana/k6@sha256:82e44a45a38ed22bf5636fe50fe8a07967c3074f7aa66567c6a7501ab9bb3a9f /usr/bin/k6 /usr/bin/k6

ENTRYPOINT ["/usr/bin/flagger-k6-webhook"]
USER 65534
