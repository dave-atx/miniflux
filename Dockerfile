ARG GO_VERSION=1
FROM golang:${GO_VERSION}-bookworm AS builder
ADD . /go/src/app
WORKDIR /go/src/app
RUN make miniflux


FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /go/src/app/miniflux /usr/bin/miniflux
CMD ["/usr/bin/miniflux"]
