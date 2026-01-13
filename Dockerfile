# syntax=docker/dockerfile:1.7

FROM golang:1.22-alpine AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN mkdir -p /out/rootfs/etc/ssl/certs \
    && cp /etc/ssl/certs/ca-certificates.crt /out/rootfs/etc/ssl/certs/ \
    && CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/rootfs/wecom-home-ops ./cmd/wecom-home-ops

FROM scratch

COPY --from=build /out/rootfs/ /

EXPOSE 8080

USER 65532:65532

ENTRYPOINT ["/wecom-home-ops"]
CMD ["-config", "/config/config.yaml"]
