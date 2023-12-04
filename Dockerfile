FROM --platform=$TARGETPLATFORM golang:1.20 as devel
ARG BUILD_ARGS
COPY / /go/src/
RUN cd /go/src/ && make build-local BUILD_ARGS=$BUILD_ARGS

FROM --platform=$TARGETPLATFORM alpine
COPY --from=devel /go/src/baetyl-gateway /
ENTRYPOINT ["./baetyl-gateway"]