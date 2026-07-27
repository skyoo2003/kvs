FROM golang:1.26-alpine AS builder

ENV CGO_ENABLED=0
ENV GO111MODULE=on

RUN apk --no-cache --no-progress add git

WORKDIR /github.com/skyoo2003/kvs
COPY . .
RUN make build

FROM alpine:3.23

RUN adduser --system --home /kvs appuser
VOLUME /kvs
WORKDIR /kvs
COPY --from=builder /github.com/skyoo2003/kvs/dist/kvs /usr/bin/kvs

EXPOSE 3456

USER appuser
ENTRYPOINT ["kvs"]
