FROM docker.io/library/golang:trixie AS build
ADD . /go/src/app
WORKDIR /go/src/app
RUN make


FROM gcr.io/distroless/static-debian13:nonroot

EXPOSE 8080
ENV LISTEN_ADDR 0.0.0.0:8080
COPY --from=builder /go/src/app/miniflux /usr/bin/miniflux
CMD ["/usr/bin/miniflux"]
