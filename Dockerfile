FROM golang:1.26.2-alpine@sha256:f85330846cde1e57ca9ec309382da3b8e6ae3ab943d2739500e08c86393a21b1 AS build

ARG GO_LDFLAGS

RUN mkdir /app
WORKDIR /app
COPY . /app/
RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux go build -ldflags "${GO_LDFLAGS} -extldflags '-static'" -o /app/flagger-k6-webhook cmd/main.go

FROM alpine:3.23@sha256:5b10f432ef3da1b8d4c7eb6c487f2f5a8f096bc91145e68878dd4a5019afde11

COPY --from=build /app/flagger-k6-webhook /usr/bin/flagger-k6-webhook
COPY --from=grafana/k6@sha256:82e44a45a38ed22bf5636fe50fe8a07967c3074f7aa66567c6a7501ab9bb3a9f /usr/bin/k6 /usr/bin/k6

ENTRYPOINT ["/usr/bin/flagger-k6-webhook"]
USER 65534
