ARG GO_VERSION=1
FROM golang:${GO_VERSION}-alpine as builder

RUN apk add --no-cache build-base git make
ADD . /go/src/app
WORKDIR /go/src/app
RUN make miniflux


FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata
COPY --from=builder /go/src/app/miniflux /usr/bin/miniflux
USER 65534
CMD ["/usr/bin/miniflux"]
