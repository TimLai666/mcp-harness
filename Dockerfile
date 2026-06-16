FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/mcp-harness ./cmd/mcp-harness
RUN go build -o /out/mcp-harness-web ./cmd/mcp-harness-web

FROM alpine:3.22

RUN apk add --no-cache git
WORKDIR /app
COPY --from=build /out/mcp-harness /app/mcp-harness
COPY --from=build /out/mcp-harness-web /app/mcp-harness-web
COPY prompts /app/prompts

ENV MCP_HARNESS_HOME=/data
EXPOSE 8765
CMD ["/app/mcp-harness-web"]
