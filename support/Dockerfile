FROM alpine:3.21

WORKDIR /opt/bupamon
RUN apk add --no-cache fping
COPY bupamon /opt/bupamon/bupamon
COPY config.yaml /opt/bupamon/config.yaml
ENTRYPOINT ["/opt/bupamon/bupamon"]
