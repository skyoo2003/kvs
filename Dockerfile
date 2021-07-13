FROM golang:1.17beta1-alpine3.12 AS builder

ENV CGO_ENABLED=0
ENV GO111MODULE=on

RUN apk --no-cache --no-progress add --virtual \
  build-deps \
  build-base \
  git

WORKDIR /github.com/skyoo2003/kvs
COPY . .
RUN make build

FROM alpine:3.14.0


VOLUME /kvs
WORKDIR /kvs
COPY --from=builder /github.com/skyoo2003/kvs/dist/kvs /usr/bin/kvs

EXPOSE 3456

ENTRYPOINT ["kvs"]
