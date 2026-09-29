FROM golang:1.27-alpine AS builder

ENV CGO_ENABLED=0
ENV GO111MODULE=on

RUN apk --no-cache --no-progress add make

WORKDIR /github.com/skyoo2003/kvs
COPY . .
RUN make build
RUN ./scripts/collect-third-party-licenses.sh /third_party_licenses

FROM alpine:3.23

WORKDIR /kvs
COPY --from=builder /github.com/skyoo2003/kvs/dist/kvs /usr/bin/kvs
COPY LICENSE NOTICE /usr/share/licenses/kvs/
COPY --from=builder /third_party_licenses /usr/share/licenses/kvs/THIRD_PARTY_LICENSES

EXPOSE 3456 3457 6379

USER 65534:65534
ENTRYPOINT ["kvs"]
CMD ["serve"]
