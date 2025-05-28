FROM golang:1.24-bullseye AS builder
WORKDIR /app
COPY . .
ENV CGO_ENABLED=1 GOOS=linux
RUN go build -o out/status-checker main.go

# https://hub.docker.com/r/minhdvu/toolbox
# https://github.com/0xPolygon/kurtosis-cdk/blob/main/docker/toolbox.Dockerfile
FROM minhdvu/toolbox:0.0.8
COPY --from=builder /app/out/status-checker /usr/bin/status-checker
EXPOSE 9090
CMD ["status-checker"]
