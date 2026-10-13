FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /moodle-mcp ./cmd/moodle-mcp

FROM alpine:3.22
RUN apk add --no-cache ca-certificates && addgroup -S app && adduser -S -G app app
COPY --from=builder /moodle-mcp /usr/local/bin/moodle-mcp
RUN mkdir /data && chown app:app /data
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/moodle-mcp"]
