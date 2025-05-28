FROM golang:1.24-bullseye AS builder
WORKDIR /app
COPY . .
ENV GOOS=linux
RUN go build -o status-checker main.go

# https://hub.docker.com/r/leovct/toolbox
FROM leovct/toolbox:0.0.8 
COPY --from=builder /app/status-checker /usr/bin/status-checker
EXPOSE 9090
CMD ["status-checker"]
