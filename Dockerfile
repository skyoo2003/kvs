FROM golang:1.15.6-alpine3.12 AS builder

ENV CGO_ENABLED=0
ENV GO111MODULE=on

RUN apk --no-cache --no-progress add --virtual \
  build-deps \
  build-base \
  git

WORKDIR /github.com/skyoo2003/kvs
COPY . .
RUN make build

FROM alpine:3.12

RUN apk --no-cache --no-progress add \
    ca-certificates \
    curl \
    git \
    openssh

VOLUME /kvs
WORKDIR /kvs
COPY --from=builder /github.com/skyoo2003/kvs/dist/kvs /usr/bin/kvs

EXPOSE 3456

ENTRYPOINT ["kvs"]
