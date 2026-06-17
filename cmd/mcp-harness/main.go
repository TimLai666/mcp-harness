package main

import (
	"context"
	"log"

	"github.com/TimLai666/mcp-harness/internal/harness"
	"github.com/TimLai666/mcp-harness/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	server := mcpserver.New(harness.NewRuntime())
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}
