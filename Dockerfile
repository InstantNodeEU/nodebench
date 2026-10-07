FROM golang:1.26-alpine AS build
WORKDIR /src
COPY server/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /nodebench-server .

FROM alpine:3.22
RUN mkdir /data && chown 65534:65534 /data
COPY --from=build /nodebench-server /usr/local/bin/nodebench-server
COPY nodebench.sh /srv/nodebench.sh
ENV NODEBENCH_LISTEN=:8080 \
    NODEBENCH_DATA=/data \
    NODEBENCH_SCRIPT=/srv/nodebench.sh
VOLUME /data
EXPOSE 8080
USER 65534
ENTRYPOINT ["nodebench-server"]
