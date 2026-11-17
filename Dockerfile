FROM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build

ARG GO_LDFLAGS

RUN apk add --no-cache 'ca-certificates>=20261020-r0' \
    && update-ca-certificates

RUN mkdir /app
WORKDIR /app
COPY . /app/
RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux go build -ldflags "${GO_LDFLAGS} -extldflags '-static'" -o /app/flagger-k6-webhook cmd/main.go

FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf
RUN apk add --no-cache 'ca-certificates>=20261020-r0' \
    && update-ca-certificates

COPY --from=build /app/flagger-k6-webhook /usr/bin/flagger-k6-webhook
COPY --from=grafana/k6:2.3.0@sha256:2dd4756ec6e14e62778916e1eb850108e4b499304c02df56760fa0bdb0fba0b8 /usr/bin/k6 /usr/bin/k6

ENTRYPOINT ["/usr/bin/flagger-k6-webhook"]
USER 65534
