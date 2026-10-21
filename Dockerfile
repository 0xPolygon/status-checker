FROM golang:1.26.10-bookworm AS builder
WORKDIR /app
COPY . .
ENV CGO_ENABLED=1 GOOS=linux
RUN go install github.com/antithesishq/antithesis-sdk-go/tools/antithesis-go-instrumentor@latest \
  && mkdir ../gen \
  && $(go env GOPATH)/bin/antithesis-go-instrumentor . ../gen \
  && cd ../gen/customer \
  && go build -o status-checker .

# https://hub.docker.com/r/minhdvu/toolbox
# https://github.com/0xPolygon/kurtosis-cdk/blob/main/docker/toolbox.Dockerfile
FROM europe-west2-docker.pkg.dev/prj-polygonlabs-devtools-dev/public/toolbox:0.0.12
COPY --from=builder /gen/customer/status-checker /usr/bin/status-checker
COPY --from=builder /gen/symbols/ /symbols/
# Same pinned toolchain as the builder, rather than the distro's golang-go.
COPY --from=builder /usr/local/go /usr/local/go
ENV PATH=/usr/local/go/bin:$PATH
EXPOSE 9090
CMD ["status-checker"]
