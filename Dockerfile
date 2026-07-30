FROM golang:1.26-alpine AS builder

ENV CGO_ENABLED=0
ENV GO111MODULE=on

RUN apk --no-cache --no-progress add make

WORKDIR /github.com/skyoo2003/kvs
COPY . .
RUN make build

FROM alpine:3.23

WORKDIR /kvs
COPY --from=builder /github.com/skyoo2003/kvs/dist/kvs /usr/bin/kvs

EXPOSE 3456 3457 6379

USER 65534:65534
ENTRYPOINT ["kvs"]
CMD ["serve"]
