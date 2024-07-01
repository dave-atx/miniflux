ARG GO_VERSION=1.22.4
FROM golang:${GO_VERSION}-bookworm AS builder
ADD . /go/src/app
WORKDIR /go/src/app
RUN make


FROM gcr.io/distroless/static-debian12:nonroot

EXPOSE 8080
ENV LISTEN_ADDR 0.0.0.0:8080
COPY --from=builder /go/src/app/miniflux /usr/bin/miniflux
CMD ["/usr/bin/miniflux"]
