package main

import (
	"bufio"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Version information
const (
	Version = "1.0.0"
	AppName = "memcc"
	Author  = "ushell"
	RepoURL = "https://github.com/ushell/tools/memcache/memcc"
)

// ANSI color codes
const (
	colorReset  = "\033[0m"
	colorRed    = "\033[31m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorBlue   = "\033[34m"
	colorPurple = "\033[35m"
	colorCyan   = "\033[36m"
	colorWhite  = "\033[37m"
	colorBold   = "\033[1m"
	colorDim    = "\033[2m"
)

// Box drawing characters
const (
	boxTopLeft     = "╭"
	boxTopRight    = "╮"
	boxBottomLeft  = "╰"
	boxBottomRight = "╯"
	boxHorizontal  = "─"
	boxVertical    = "│"
	boxTeeRight    = "├"
	boxTeeLeft     = "┤"
	boxTeeDown     = "┬"
	boxTeeUp       = "┴"
	boxCross       = "┼"
)

// MemcachedClient is a simple Memcached client
type MemcachedClient struct {
	conn net.Conn
	host string
	port int
}

// NewMemcachedClient creates a new Memcached client connection
func NewMemcachedClient(host string, port int) (*MemcachedClient, error) {
	address := net.JoinHostPort(host, strconv.Itoa(port))
	conn, err := net.DialTimeout("tcp", address, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Memcached server: %v", err)
	}

	return &MemcachedClient{conn: conn, host: host, port: port}, nil
}

// Close closes the connection to Memcached server
func (c *MemcachedClient) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// Get retrieves the value for a given key from Memcached
func (c *MemcachedClient) Get(key string) (string, error) {
	if c.conn == nil {
		return "", fmt.Errorf("client not connected")
	}

	cmd := fmt.Sprintf("get %s\r\n", key)
	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		return "", fmt.Errorf("failed to send get command: %v", err)
	}

	reader := bufio.NewReader(c.conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read response: %v", err)
	}

	if strings.HasPrefix(line, "END") {
		return "", nil
	}

	parts := strings.Fields(line)
	if len(parts) != 4 || parts[0] != "VALUE" {
		return "", fmt.Errorf("invalid response format: %s", line)
	}

	valueLength, err := strconv.Atoi(parts[3])
	if err != nil {
		return "", fmt.Errorf("invalid value length: %v", err)
	}

	valueBytes := make([]byte, valueLength)
	_, err = reader.Read(valueBytes)
	if err != nil {
		return "", fmt.Errorf("failed to read value: %v", err)
	}

	_, err = reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read newline: %v", err)
	}

	endLine, err := reader.ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("failed to read end marker: %v", err)
	}

	if !strings.HasPrefix(endLine, "END") {
		return "", fmt.Errorf("end marker not found: %s", endLine)
	}

	return string(valueBytes), nil
}

// Set stores a key-value pair in Memcached
func (c *MemcachedClient) Set(key string, value string, expTime int) error {
	if c.conn == nil {
		return fmt.Errorf("client not connected")
	}

	cmd := fmt.Sprintf("set %s 0 %d %d\r\n%s\r\n", key, expTime, len(value), value)
	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		return fmt.Errorf("failed to send set command: %v", err)
	}

	reader := bufio.NewReader(c.conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "STORED") {
		return fmt.Errorf("failed to set value: %s", strings.TrimSpace(response))
	}

	return nil
}

// Delete removes a key from Memcached
func (c *MemcachedClient) Delete(key string) error {
	if c.conn == nil {
		return fmt.Errorf("client not connected")
	}

	cmd := fmt.Sprintf("delete %s\r\n", key)
	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		return fmt.Errorf("failed to send delete command: %v", err)
	}

	reader := bufio.NewReader(c.conn)
	response, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read response: %v", err)
	}

	if !strings.HasPrefix(response, "DELETED") {
		if strings.HasPrefix(response, "NOT_FOUND") {
			return fmt.Errorf("key not found")
		}
		return fmt.Errorf("failed to delete key: %s", strings.TrimSpace(response))
	}

	return nil
}

// GetKeys retrieves all keys matching the given pattern
func (c *MemcachedClient) GetKeys(pattern string) ([]string, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("client not connected")
	}

	cmd := "stats items\r\n"
	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		return nil, fmt.Errorf("failed to send stats items command: %v", err)
	}

	reader := bufio.NewReader(c.conn)
	slabIDs := make(map[string]bool)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %v", err)
		}

		if strings.HasPrefix(line, "END") {
			break
		}

		if strings.HasPrefix(line, "STAT items:") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				slabIDs[parts[1]] = true
			}
		}
	}

	var keys []string
	for slabID := range slabIDs {
		cmd = fmt.Sprintf("stats cachedump %s 0\r\n", slabID)
		_, err = c.conn.Write([]byte(cmd))
		if err != nil {
			return nil, fmt.Errorf("failed to send stats cachedump command: %v", err)
		}

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				return nil, fmt.Errorf("failed to read response: %v", err)
			}

			if strings.HasPrefix(line, "END") {
				break
			}

			if strings.HasPrefix(line, "ITEM ") {
				parts := strings.Fields(line)
				if len(parts) > 1 {
					key := parts[1]
					if pattern == "*" || strings.Contains(key, strings.Replace(pattern, "*", "", -1)) {
						keys = append(keys, key)
					}
				}
			}
		}
	}

	sort.Strings(keys)
	return keys, nil
}

// CacheItem represents a cached item with metadata
type CacheItem struct {
	Key    string
	Size   string
	Expiry string
}

// CacheDump retrieves cached items from a specific slab
func (c *MemcachedClient) CacheDump(slabID string, limit int) ([]CacheItem, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("client not connected")
	}

	cmd := fmt.Sprintf("stats cachedump %s %d\r\n", slabID, limit)
	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		return nil, fmt.Errorf("failed to send stats cachedump command: %v", err)
	}

	reader := bufio.NewReader(c.conn)
	var items []CacheItem

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %v", err)
		}

		if strings.HasPrefix(line, "END") {
			break
		}

		if strings.HasPrefix(line, "ITEM ") {
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				item := CacheItem{
					Key:    parts[1],
					Size:   strings.Trim(parts[2], "[]"),
					Expiry: parts[3],
				}
				items = append(items, item)
			}
		}
	}

	return items, nil
}

// GetAllSlabs retrieves all slab IDs
func (c *MemcachedClient) GetAllSlabs() ([]string, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("client not connected")
	}

	cmd := "stats items\r\n"
	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		return nil, fmt.Errorf("failed to send stats items command: %v", err)
	}

	reader := bufio.NewReader(c.conn)
	slabIDs := make(map[string]bool)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %v", err)
		}

		if strings.HasPrefix(line, "END") {
			break
		}

		if strings.HasPrefix(line, "STAT items:") {
			parts := strings.Split(line, ":")
			if len(parts) > 1 {
				slabIDs[parts[1]] = true
			}
		}
	}

	result := make([]string, 0, len(slabIDs))
	for slabID := range slabIDs {
		result = append(result, slabID)
	}
	sort.Strings(result)

	return result, nil
}

// Statistics retrieves server statistics
func (c *MemcachedClient) Statistics(statType string) (map[string]string, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("client not connected")
	}

	cmd := "stats"
	if statType != "" {
		cmd = fmt.Sprintf("stats %s", statType)
	}
	cmd += "\r\n"

	_, err := c.conn.Write([]byte(cmd))
	if err != nil {
		return nil, fmt.Errorf("failed to send stats command: %v", err)
	}

	reader := bufio.NewReader(c.conn)
	stats := make(map[string]string)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %v", err)
		}

		if strings.HasPrefix(line, "END") {
			break
		}

		if strings.HasPrefix(line, "STAT ") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				key := parts[1]
				value := strings.Join(parts[2:], " ")
				stats[key] = value
			}
		}
	}

	return stats, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// UI Helper Functions
// ═══════════════════════════════════════════════════════════════════════════

func printBanner() {
	banner := `
    ┌──────────────────────────────────────────────────┐
    │                                                  │
    │   ███╗   ███╗███████╗███╗   ███╗ ██████╗ ██████╗ │
    │   ████╗ ████║██╔════╝████╗ ████║██╔════╝██╔════╝ │
    │   ██╔████╔██║█████╗  ██╔████╔██║██║     ██║      │
    │   ██║╚██╔╝██║██╔══╝  ██║╚██╔╝██║██║     ██║      │
    │   ██║ ╚═╝ ██║███████╗██║ ╚═╝ ██║╚██████╗╚██████╗ │
    │   ╚═╝     ╚═╝╚══════╝╚═╝     ╚═╝ ╚═════╝ ╚═════╝ │
    │                                                  │
    │            Memcached CLI Client                  │
    └──────────────────────────────────────────────────┘`
	fmt.Println(colorCyan + banner + colorReset)
	fmt.Printf("    %sVersion %s%s\n\n", colorDim, Version, colorReset)
}

func printSuccess(message string) {
	fmt.Printf("%s%s ✓ %s%s\n", colorGreen, colorBold, message, colorReset)
}

func printError(message string) {
	fmt.Printf("%s%s ✗ %s%s\n", colorRed, colorBold, message, colorReset)
}

func printInfo(message string) {
	fmt.Printf("%s%s ℹ %s%s\n", colorBlue, colorBold, message, colorReset)
}

func printWarning(message string) {
	fmt.Printf("%s%s ⚠ %s%s\n", colorYellow, colorBold, message, colorReset)
}

func printHeader(title string) {
	width := 50
	padding := (width - len(title) - 2) / 2
	if padding < 0 {
		padding = 0
	}

	fmt.Println()
	fmt.Printf("%s%s", colorCyan, boxTopLeft)
	fmt.Print(strings.Repeat(boxHorizontal, width))
	fmt.Printf("%s%s\n", boxTopRight, colorReset)

	fmt.Printf("%s%s%s", colorCyan, boxVertical, colorReset)
	fmt.Printf("%s%s%s", strings.Repeat(" ", padding), colorBold+title+colorReset, strings.Repeat(" ", width-padding-len(title)))
	fmt.Printf("%s%s%s\n", colorCyan, boxVertical, colorReset)

	fmt.Printf("%s%s", colorCyan, boxBottomLeft)
	fmt.Print(strings.Repeat(boxHorizontal, width))
	fmt.Printf("%s%s\n", boxBottomRight, colorReset)
}

func printTableHeader(columns []string, widths []int) {
	// Top border
	fmt.Printf("%s%s", colorCyan, boxTopLeft)
	for i, w := range widths {
		fmt.Print(strings.Repeat(boxHorizontal, w+2))
		if i < len(widths)-1 {
			fmt.Print(boxTeeDown)
		}
	}
	fmt.Printf("%s%s\n", boxTopRight, colorReset)

	// Header row
	fmt.Printf("%s%s%s", colorCyan, boxVertical, colorReset)
	for i, col := range columns {
		fmt.Printf(" %s%s%-*s%s ", colorBold, colorWhite, widths[i], col, colorReset)
		fmt.Printf("%s%s%s", colorCyan, boxVertical, colorReset)
	}
	fmt.Println()

	// Header separator
	fmt.Printf("%s%s", colorCyan, boxTeeRight)
	for i, w := range widths {
		fmt.Print(strings.Repeat(boxHorizontal, w+2))
		if i < len(widths)-1 {
			fmt.Print(boxCross)
		}
	}
	fmt.Printf("%s%s\n", boxTeeLeft, colorReset)
}

func printTableRow(values []string, widths []int) {
	fmt.Printf("%s%s%s", colorCyan, boxVertical, colorReset)
	for i, val := range values {
		displayVal := val
		if len(val) > widths[i] {
			displayVal = val[:widths[i]-3] + "..."
		}
		fmt.Printf(" %-*s ", widths[i], displayVal)
		fmt.Printf("%s%s%s", colorCyan, boxVertical, colorReset)
	}
	fmt.Println()
}

func printTableFooter(widths []int) {
	fmt.Printf("%s%s", colorCyan, boxBottomLeft)
	for i, w := range widths {
		fmt.Print(strings.Repeat(boxHorizontal, w+2))
		if i < len(widths)-1 {
			fmt.Print(boxTeeUp)
		}
	}
	fmt.Printf("%s%s\n", boxBottomRight, colorReset)
}

func printCacheDump(items []CacheItem) {
	if len(items) == 0 {
		printWarning("No cached items found")
		return
	}

	printHeader("Cache Dump")

	columns := []string{"Key", "Size (bytes)", "Expiry"}
	widths := []int{35, 12, 15}

	printTableHeader(columns, widths)
	for _, item := range items {
		printTableRow([]string{item.Key, item.Size, item.Expiry}, widths)
	}
	printTableFooter(widths)

	fmt.Printf("\n%s%s Total: %d items%s\n", colorDim, colorCyan, len(items), colorReset)
}

func printStatistics(stats map[string]string) {
	if len(stats) == 0 {
		printWarning("No statistics available")
		return
	}

	printHeader("Server Statistics")

	// Sort keys
	keys := make([]string, 0, len(stats))
	for k := range stats {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	columns := []string{"Metric", "Value"}
	widths := []int{30, 25}

	printTableHeader(columns, widths)
	for _, k := range keys {
		printTableRow([]string{k, stats[k]}, widths)
	}
	printTableFooter(widths)

	fmt.Printf("\n%s%s Total: %d metrics%s\n", colorDim, colorCyan, len(stats), colorReset)
}

func printUsage() {
	printBanner()

	fmt.Printf("%s%sUSAGE%s\n", colorBold, colorYellow, colorReset)
	fmt.Printf("    %s <command> [arguments]\n\n", AppName)

	fmt.Printf("%s%sCOMMANDS%s\n", colorBold, colorYellow, colorReset)

	commands := []struct {
		cmd  string
		desc string
		args string
	}{
		{"keys", "List keys matching pattern", "<pattern>"},
		{"get", "Get value for a key", "<key>"},
		{"set", "Set a key-value pair", "<key> <value> [expiry]"},
		{"delete", "Delete a key", "<key>"},
		{"stats", "Show server statistics", "[type]"},
		{"cachedump", "Dump cache from slab", "<slab_id> [limit]"},
		{"slabs", "List all slab IDs", ""},
		{"version", "Show version info", ""},
		{"help", "Show this help message", ""},
	}

	for _, c := range commands {
		fmt.Printf("    %s%-12s%s %-25s %s%s%s\n",
			colorGreen, c.cmd, colorReset,
			c.args,
			colorDim, c.desc, colorReset)
	}

	fmt.Printf("\n%s%sEXAMPLES%s\n", colorBold, colorYellow, colorReset)

	examples := []struct {
		cmd  string
		desc string
	}{
		{AppName + " keys *", "List all keys"},
		{AppName + " get mykey", "Get value of 'mykey'"},
		{AppName + " set mykey hello 3600", "Set 'mykey' to 'hello' with 1h TTL"},
		{AppName + " delete mykey", "Delete 'mykey'"},
		{AppName + " stats", "Show all statistics"},
		{AppName + " stats items", "Show item statistics"},
		{AppName + " cachedump 1 10", "Dump first 10 items from slab 1"},
		{AppName + " slabs", "List all slab IDs"},
	}

	for _, e := range examples {
		fmt.Printf("    %s%s%s\n        %s%s%s\n",
			colorCyan, e.cmd, colorReset,
			colorDim, e.desc, colorReset)
	}

	fmt.Printf("\n%s%sGLOBAL OPTIONS%s\n", colorBold, colorYellow, colorReset)
	fmt.Printf("    %s-H, --host%s      Memcached server host (default: localhost)\n", colorGreen, colorReset)
	fmt.Printf("    %s-P, --port%s      Memcached server port (default: 11211)\n", colorGreen, colorReset)
	fmt.Printf("    %s-s, --server%s    Server address as host:port\n", colorGreen, colorReset)
	fmt.Printf("    %s    --help%s      Show this help message\n", colorGreen, colorReset)
	fmt.Printf("    %s    --version%s   Show version information\n\n", colorGreen, colorReset)

	fmt.Printf("%s%sENVIRONMENT VARIABLES%s\n", colorBold, colorYellow, colorReset)
	fmt.Printf("    %sMEMCACHED_HOST%s  Server host (overridden by -H)\n", colorGreen, colorReset)
	fmt.Printf("    %sMEMCACHED_PORT%s  Server port (overridden by -P)\n\n", colorGreen, colorReset)

	fmt.Printf("%sDefault connection: localhost:11211%s\n\n", colorDim, colorReset)
}

func printVersion() {
	fmt.Printf("\n%s%s%s v%s%s\n", colorBold, colorCyan, AppName, Version, colorReset)
	fmt.Printf("%sA fast and simple Memcached CLI client%s\n\n", colorDim, colorReset)
	fmt.Printf("  Author:  %s\n", Author)
	fmt.Printf("  Repo:    %s%s%s\n\n", colorBlue, RepoURL, colorReset)
}

// Config holds the connection configuration
type Config struct {
	Host string
	Port int
}

// getDefaultConfig returns default configuration with environment variable overrides
func getDefaultConfig() Config {
	cfg := Config{
		Host: "localhost",
		Port: 11211,
	}

	// Check environment variables
	if envHost := os.Getenv("MEMCACHED_HOST"); envHost != "" {
		cfg.Host = envHost
	}
	if envPort := os.Getenv("MEMCACHED_PORT"); envPort != "" {
		if p, err := strconv.Atoi(envPort); err == nil {
			cfg.Port = p
		}
	}

	return cfg
}

// parseArgs parses command line arguments and returns config, command, and remaining args
func parseArgs() (Config, string, []string) {
	cfg := getDefaultConfig()

	// Define flags
	fs := flag.NewFlagSet(AppName, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)

	// Connection flags
	hostFlag := fs.String("H", "", "Memcached server host")
	hostLongFlag := fs.String("host", "", "Memcached server host")
	portFlag := fs.Int("P", 0, "Memcached server port")
	portLongFlag := fs.Int("port", 0, "Memcached server port")
	serverFlag := fs.String("s", "", "Server address as host:port")
	serverLongFlag := fs.String("server", "", "Server address as host:port")

	// Help/version flags
	helpFlag := fs.Bool("help", false, "Show help message")
	versionFlag := fs.Bool("version", false, "Show version")

	// Find the first non-flag argument (command)
	var commandIdx int
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if !strings.HasPrefix(arg, "-") {
			commandIdx = i
			break
		}
		// Skip the value of flags that take arguments
		if arg == "-H" || arg == "-P" || arg == "-s" ||
			arg == "--host" || arg == "--port" || arg == "--server" {
			i++ // skip next argument (the value)
		}
	}

	// Parse flags before the command
	if commandIdx > 1 {
		if err := fs.Parse(os.Args[1:commandIdx]); err != nil {
			if err == flag.ErrHelp {
				printUsage()
				os.Exit(0)
			}
			os.Exit(1)
		}
	} else if commandIdx == 0 {
		// No command found, parse all args as flags
		if err := fs.Parse(os.Args[1:]); err != nil {
			if err == flag.ErrHelp {
				printUsage()
				os.Exit(0)
			}
			os.Exit(1)
		}
	}

	// Check help/version flags
	if *helpFlag {
		printUsage()
		os.Exit(0)
	}
	if *versionFlag {
		printVersion()
		os.Exit(0)
	}

	// Apply server flag (host:port combined)
	serverAddr := *serverFlag
	if *serverLongFlag != "" {
		serverAddr = *serverLongFlag
	}
	if serverAddr != "" {
		host, portStr, err := net.SplitHostPort(serverAddr)
		if err != nil {
			printError(fmt.Sprintf("Invalid server address: %s", serverAddr))
			os.Exit(1)
		}
		cfg.Host = host
		if p, err := strconv.Atoi(portStr); err == nil {
			cfg.Port = p
		}
	}

	// Apply individual host/port flags (override server flag)
	if *hostFlag != "" {
		cfg.Host = *hostFlag
	}
	if *hostLongFlag != "" {
		cfg.Host = *hostLongFlag
	}
	if *portFlag != 0 {
		cfg.Port = *portFlag
	}
	if *portLongFlag != 0 {
		cfg.Port = *portLongFlag
	}

	// Get command and remaining args
	var command string
	var args []string
	if commandIdx > 0 && commandIdx < len(os.Args) {
		command = os.Args[commandIdx]
		args = os.Args[commandIdx+1:]
	}

	return cfg, command, args
}

func main() {
	cfg, command, args := parseArgs()

	// Handle no command
	if command == "" {
		printUsage()
		return
	}

	// Handle help and version commands
	switch command {
	case "help":
		printUsage()
		return
	case "version":
		printVersion()
		return
	}

	// Create Memcached client
	client, err := NewMemcachedClient(cfg.Host, cfg.Port)
	if err != nil {
		printError(fmt.Sprintf("Failed to connect: %v", err))
		os.Exit(1)
	}
	defer client.Close()

	printInfo(fmt.Sprintf("Connected to %s:%d", client.host, client.port))

	switch command {
	case "keys":
		if len(args) < 1 {
			printError("Missing pattern argument")
			fmt.Printf("\n%sUsage: %s [options] keys <pattern>%s\n", colorDim, AppName, colorReset)
			os.Exit(1)
		}
		pattern := args[0]
		keys, err := client.GetKeys(pattern)
		if err != nil {
			printError(fmt.Sprintf("Failed to get keys: %v", err))
			os.Exit(1)
		}
		if len(keys) == 0 {
			printWarning("No matching keys found")
		} else {
			printHeader(fmt.Sprintf("Keys matching '%s'", pattern))
			for i, key := range keys {
				fmt.Printf("  %s%3d.%s %s\n", colorDim, i+1, colorReset, key)
			}
			fmt.Printf("\n%s%s Total: %d keys%s\n", colorDim, colorCyan, len(keys), colorReset)
		}

	case "get":
		if len(args) < 1 {
			printError("Missing key argument")
			fmt.Printf("\n%sUsage: %s [options] get <key>%s\n", colorDim, AppName, colorReset)
			os.Exit(1)
		}
		key := args[0]
		value, err := client.Get(key)
		if err != nil {
			printError(fmt.Sprintf("Failed to get value: %v", err))
			os.Exit(1)
		}
		if value == "" {
			printWarning(fmt.Sprintf("Key '%s' not found", key))
		} else {
			printHeader(fmt.Sprintf("Value for '%s'", key))
			fmt.Printf("\n%s\n\n", value)
			printSuccess(fmt.Sprintf("Retrieved %d bytes", len(value)))
		}

	case "set":
		if len(args) < 2 {
			printError("Missing key or value argument")
			fmt.Printf("\n%sUsage: %s [options] set <key> <value> [expiry]%s\n", colorDim, AppName, colorReset)
			os.Exit(1)
		}
		key := args[0]
		value := args[1]
		expTime := 0
		if len(args) > 2 {
			expTime, _ = strconv.Atoi(args[2])
		}
		err := client.Set(key, value, expTime)
		if err != nil {
			printError(fmt.Sprintf("Failed to set value: %v", err))
			os.Exit(1)
		}
		ttlMsg := "no expiration"
		if expTime > 0 {
			ttlMsg = fmt.Sprintf("TTL: %ds", expTime)
		}
		printSuccess(fmt.Sprintf("Set '%s' = '%s' (%s)", key, value, ttlMsg))

	case "delete", "del", "rm":
		if len(args) < 1 {
			printError("Missing key argument")
			fmt.Printf("\n%sUsage: %s [options] delete <key>%s\n", colorDim, AppName, colorReset)
			os.Exit(1)
		}
		key := args[0]
		err := client.Delete(key)
		if err != nil {
			printError(fmt.Sprintf("Failed to delete key: %v", err))
			os.Exit(1)
		}
		printSuccess(fmt.Sprintf("Deleted key '%s'", key))

	case "stats":
		statType := ""
		if len(args) > 0 {
			statType = args[0]
		}
		stats, err := client.Statistics(statType)
		if err != nil {
			printError(fmt.Sprintf("Failed to get statistics: %v", err))
			os.Exit(1)
		}
		printStatistics(stats)

	case "cachedump", "dump":
		if len(args) < 1 {
			printError("Missing slab ID argument")
			fmt.Printf("\n%sUsage: %s [options] cachedump <slab_id> [limit]%s\n", colorDim, AppName, colorReset)
			os.Exit(1)
		}
		slabID := args[0]
		limit := 0
		if len(args) > 1 {
			limit, _ = strconv.Atoi(args[1])
		}
		items, err := client.CacheDump(slabID, limit)
		if err != nil {
			printError(fmt.Sprintf("Failed to dump cache: %v", err))
			os.Exit(1)
		}
		printCacheDump(items)

	case "slabs":
		slabs, err := client.GetAllSlabs()
		if err != nil {
			printError(fmt.Sprintf("Failed to get slab IDs: %v", err))
			os.Exit(1)
		}
		if len(slabs) == 0 {
			printWarning("No slabs found")
		} else {
			printHeader("Slab IDs")
			for i, slabID := range slabs {
				fmt.Printf("  %s%3d.%s Slab %s%s%s\n", colorDim, i+1, colorReset, colorGreen, slabID, colorReset)
			}
			fmt.Printf("\n%s%s Total: %d slabs%s\n", colorDim, colorCyan, len(slabs), colorReset)
		}

	default:
		printError(fmt.Sprintf("Unknown command: %s", command))
		fmt.Printf("\n%sRun '%s help' for usage information%s\n", colorDim, AppName, colorReset)
		os.Exit(1)
	}
}

