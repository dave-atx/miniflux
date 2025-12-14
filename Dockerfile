ARG GO_VERSION=1
FROM golang:${GO_VERSION}-trixie AS builder
ADD . /go/src/app
WORKDIR /go/src/app
RUN make miniflux


FROM gcr.io/distroless/base-debian13:nonroot

EXPOSE 8080
ENV LISTEN_ADDR 0.0.0.0:8080
COPY --from=builder /go/src/app/miniflux /usr/bin/miniflux
CMD ["/usr/bin/miniflux"]
