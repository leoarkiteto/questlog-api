# Build stage — templ-generated code and compiled CSS are committed, so
# the image only needs the Go toolchain.
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/questlog .

# Runtime stage — Render sets PORT automatically; the app reads it at startup.
FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
 && adduser -D -u 10001 app
COPY --from=build /out/questlog /usr/local/bin/questlog
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/questlog"]
