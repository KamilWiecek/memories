FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
ENV GONOSUMDB=* GOFLAGS=-mod=mod
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /cronjob ./cmd/cronjob

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /cronjob /cronjob
ENTRYPOINT ["/cronjob"]