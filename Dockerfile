FROM golang:1.27.3-alpine@sha256:d200d2dd76b03a99b52651fd1325aff7dcd35422c35c01bac56571b0507f3deb AS build

ARG GO_LDFLAGS

RUN apk add --no-cache 'ca-certificates>=20261020-r0' \
    && update-ca-certificates

RUN mkdir /app
WORKDIR /app
COPY . /app/
RUN --mount=type=cache,target=/go/pkg/mod CGO_ENABLED=0 GOOS=linux go build -ldflags "${GO_LDFLAGS} -extldflags '-static'" -o /app/flagger-k6-webhook cmd/main.go

FROM alpine:3.24@sha256:be2656061b536c1883c4034790e21d0a09c5aaaf51968b89368a3c0371ea2c9a
RUN apk add --no-cache 'ca-certificates>=20261020-r0' \
    && update-ca-certificates

COPY --from=build /app/flagger-k6-webhook /usr/bin/flagger-k6-webhook
COPY --from=grafana/k6:2.3.0@sha256:2dd4756ec6e14e62778916e1eb850108e4b499304c02df56760fa0bdb0fba0b8 /usr/bin/k6 /usr/bin/k6

ENTRYPOINT ["/usr/bin/flagger-k6-webhook"]
USER 65534
