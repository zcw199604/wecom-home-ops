# syntax=docker/dockerfile:1.7

FROM golang:1.22-alpine AS build

ARG TARGETOS=linux
ARG TARGETARCH=amd64

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/daily-help ./cmd/daily-help

FROM gcr.io/distroless/static:nonroot

COPY --from=build /out/daily-help /daily-help

EXPOSE 8080

USER nonroot:nonroot

ENTRYPOINT ["/daily-help"]
CMD ["-config", "/config/config.yaml"]
