# syntax=docker/dockerfile:1
ARG GO_VERSION=1
FROM golang:${GO_VERSION}-trixie AS builder
WORKDIR /go/src/app
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    make miniflux


FROM gcr.io/distroless/base-debian13:nonroot

EXPOSE 8080
ENV LISTEN_ADDR=0.0.0.0:8080
COPY --from=builder /go/src/app/miniflux /usr/bin/miniflux
CMD ["/usr/bin/miniflux"]
