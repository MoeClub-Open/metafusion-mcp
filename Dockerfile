FROM golang:1.27.2-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mcp ./cmd/mcp
FROM alpine:3.22
RUN apk add --no-cache ca-certificates && adduser -D -u 10001 mcp
COPY --from=build /out/mcp /usr/local/bin/mcp
USER mcp
EXPOSE 8084
ENTRYPOINT ["mcp"]
