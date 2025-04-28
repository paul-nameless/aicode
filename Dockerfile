# syntax=docker/dockerfile:1

# Builder stage
FROM golang:1.24.2-alpine3.21 AS builder
RUN apk update && apk add --no-cache git bash
WORKDIR /src

# Download dependencies
COPY go.mod go.sum ./

# Build the application
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /aicode

# Final stage
FROM alpine:3.21
RUN apk update && apk add --no-cache git fd ripgrep ca-certificates
WORKDIR /app
COPY --from=builder /aicode /aicode

ENTRYPOINT ["/aicode"]
