FROM golang:1.25-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /out/mcp-harness ./cmd/mcp-harness
RUN go build -o /out/mcp-harness-web ./cmd/mcp-harness-web

FROM alpine:3.24 AS runtime

# Install system deps and runtimes
RUN apk add --no-cache \
    git github-cli ripgrep \
    nodejs npm \
    go

# Install uv (standalone installer)
RUN wget -qO- https://astral.sh/uv/install.sh | sh && \
    ln -s /root/.local/bin/uv /usr/local/bin/uv && \
    uv --version

# Install bun (via npm)
RUN npm install -g bun && \
    bun --version

WORKDIR /app
COPY --from=build /out/mcp-harness /app/mcp-harness
COPY --from=build /out/mcp-harness-web /app/mcp-harness-web
COPY prompts /app/prompts

ENV MCP_HARNESS_HOME=/data
EXPOSE 8765
CMD ["/app/mcp-harness-web"]
