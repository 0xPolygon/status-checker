FROM golang:1.24-bullseye AS builder
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
FROM minhdvu/toolbox:0.0.8
COPY --from=builder /gen/customer/status-checker /usr/bin/status-checker
EXPOSE 9090
CMD ["status-checker"]
