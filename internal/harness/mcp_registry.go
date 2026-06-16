package harness

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type mcpConfigFile struct {
	Servers []MCPServerConfig `json:"servers"`
}

func LoadMCPServers() ([]MCPServerConfig, error) {
	path, err := MCPsPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var payload mcpConfigFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, err
	}
	return payload.Servers, nil
}

func SaveMCPServers(servers []MCPServerConfig) error {
	path, err := MCPsPath()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(mcpConfigFile{Servers: servers}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func AddMCPServer(config MCPServerConfig) error {
	if config.ID == "" || config.Name == "" {
		return errors.New("mcp server id and name are required")
	}
	if config.Transport == "" {
		config.Transport = "stdio"
	}
	if config.Transport != "stdio" && config.Transport != "streamable_http" {
		return errors.New("supported transports: stdio, streamable_http")
	}
	servers, err := LoadMCPServers()
	if err != nil {
		return err
	}
	replaced := false
	for i := range servers {
		if servers[i].ID == config.ID {
			servers[i] = config
			replaced = true
		}
	}
	if !replaced {
		servers = append(servers, config)
	}
	return SaveMCPServers(servers)
}

func DeleteMCPServer(id string) error {
	servers, err := LoadMCPServers()
	if err != nil {
		return err
	}
	next := servers[:0]
	for _, server := range servers {
		if server.ID != id {
			next = append(next, server)
		}
	}
	return SaveMCPServers(next)
}

func FindMCPServer(id string) (MCPServerConfig, error) {
	servers, err := LoadMCPServers()
	if err != nil {
		return MCPServerConfig{}, err
	}
	for _, server := range servers {
		if server.ID == id || server.Name == id {
			return server, nil
		}
	}
	return MCPServerConfig{}, errors.New("unknown MCP server: " + id)
}

func MCPTransport(config MCPServerConfig) (mcp.Transport, error) {
	switch config.Transport {
	case "", "stdio":
		if config.Command == "" {
			return nil, errors.New("stdio MCP server requires command")
		}
		cmd := exec.Command(config.Command, config.Args...)
		if len(config.Env) > 0 {
			env := os.Environ()
			for key, value := range config.Env {
				env = append(env, key+"="+value)
			}
			cmd.Env = env
		}
		return &mcp.CommandTransport{Command: cmd}, nil
	case "streamable_http":
		if config.Endpoint == "" {
			return nil, errors.New("streamable_http MCP server requires endpoint")
		}
		return &mcp.StreamableClientTransport{Endpoint: config.Endpoint}, nil
	default:
		return nil, errors.New("unsupported MCP transport: " + config.Transport)
	}
}

func MCPTimeout(args map[string]any) time.Duration {
	timeout := time.Duration(getInt(args, "timeout_ms", 30000)) * time.Millisecond
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return timeout
}

func MCPToolFullName(serverID, tool string) string {
	return strings.TrimSpace(serverID) + "." + strings.TrimSpace(tool)
}
