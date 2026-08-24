package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zelentsov-dev/apple-ads-mcp/internal/appleads"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/config"
	"github.com/zelentsov-dev/apple-ads-mcp/internal/mcpserver"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "apple-ads-mcp:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return usageError()
	}
	switch args[0] {
	case "serve":
		return serve(ctx, args[1:])
	case "config":
		if len(args) > 1 && args[1] == "init" {
			return configInit(args[2:])
		}
	case "auth":
		if len(args) > 1 && args[1] == "doctor" {
			return authDoctor(ctx, args[2:])
		}
	case "accounts":
		if len(args) > 1 && args[1] == "discover" {
			return accountsDiscover(ctx, args[2:])
		}
	case "version", "--version", "-version":
		fmt.Println(mcpserver.Version)
		return nil
	}
	return usageError()
}

func serve(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	stdio := flags.Bool("stdio", false, "serve MCP over standard input and output")
	configPath := flags.String("config", "", "absolute path to accounts.json")
	allowWrites := flags.Bool("allow-writes", false, "enable receipt-gated mutations")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if !*stdio {
		return errors.New("serve currently requires --stdio")
	}
	cfg, source, err := config.LoadOptional(*configPath)
	if err != nil {
		return err
	}
	return mcpserver.RunStdio(ctx, appleads.NewManager(cfg, source), *allowWrites)
}

func configInit(args []string) error {
	flags := flag.NewFlagSet("config init", flag.ContinueOnError)
	path := flags.String("path", "", "absolute config destination")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		defaultPath, err := config.DefaultPath()
		if err != nil {
			return err
		}
		*path = defaultPath
	}
	absPath, err := filepath.Abs(*path)
	if err != nil {
		return fmt.Errorf("resolve config path: %w", err)
	}
	if _, err := os.Stat(absPath); err == nil {
		return fmt.Errorf("config already exists at %s", absPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	reader := bufio.NewReader(os.Stdin)
	profile := config.Profile{}
	profile.Name = prompt(reader, "Profile name", "default")
	profile.ClientID = prompt(reader, "Client ID", "")
	profile.TeamID = prompt(reader, "Team ID", "")
	profile.KeyID = prompt(reader, "Key ID", "")
	profile.PrivateKeyPath = prompt(reader, "Absolute private key path", "")
	profile.DefaultAdAccountID = prompt(reader, "Default ad account ID (optional)", "")
	profile.AllowWrites = strings.EqualFold(prompt(reader, "Allow receipt-gated writes for this profile? (yes/no)", "no"), "yes")
	if err := profile.ValidatePrivateKeyFile(); err != nil {
		return err
	}
	if err := config.Save(absPath, config.Config{Profiles: []config.Profile{profile}}); err != nil {
		return err
	}
	fmt.Printf("Configuration saved to %s\n", absPath)
	return nil
}

func authDoctor(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("auth doctor", flag.ContinueOnError)
	profile := flags.String("profile", "", "profile name")
	configPath := flags.String("config", "", "absolute path to accounts.json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, source, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	manager := appleads.NewManager(cfg, source)
	result, err := manager.Do(ctx, *profile, "", appleads.Me())
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"status": "ok", "profile": *profile, "identity": result.Data})
}

func accountsDiscover(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("accounts discover", flag.ContinueOnError)
	profile := flags.String("profile", "", "profile name")
	configPath := flags.String("config", "", "absolute path to accounts.json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	cfg, source, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	result, err := appleads.NewManager(cfg, source).Do(ctx, *profile, "", appleads.ACLs())
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"profile": *profile, "accounts": result.Data, "pagination": result.Pagination})
}

func prompt(reader *bufio.Reader, label, fallback string) string {
	if fallback == "" {
		fmt.Fprintf(os.Stderr, "%s: ", label)
	} else {
		fmt.Fprintf(os.Stderr, "%s [%s]: ", label, fallback)
	}
	value, _ := reader.ReadString('\n')
	value = strings.TrimSpace(value)
	if value == "" {
		return fallback
	}
	return value
}

func printJSON(value any) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usageError() error {
	return errors.New("usage: apple-ads-mcp {serve --stdio|config init|auth doctor|accounts discover|version}")
}
