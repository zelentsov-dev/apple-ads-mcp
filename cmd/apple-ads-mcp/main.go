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
	"github.com/zelentsov-dev/apple-ads-mcp/internal/optimization"
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
	case "optimization":
		if len(args) > 2 && args[1] == "policy" && args[2] == "init" {
			return optimizationPolicyInit(args[3:])
		}
		if len(args) > 2 && args[1] == "policy" && args[2] == "validate" {
			return optimizationPolicyValidate(args[3:])
		}
		if len(args) > 1 && args[1] == "doctor" {
			return optimizationDoctor(ctx, args[2:])
		}
	case "billing":
		if len(args) > 2 && args[1] == "profile" && args[2] == "init" {
			return billingProfileInit(args[3:])
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
	allowDeletes := flags.Bool("allow-deletes", false, "enable separately gated destructive previews and applies")
	policyPath := flags.String("optimization-policies", "", "absolute path to optimization-policies.json")
	billingPath := flags.String("billing-profiles", "", "absolute path to billing-profiles.json")
	historyRoot := flags.String("optimization-state", "", "absolute optimization state directory")
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
	return mcpserver.RunStdioWithOptions(ctx, appleads.NewManager(cfg, source), mcpserver.Options{
		AllowWrites: *allowWrites, AllowDeletes: *allowDeletes, PolicyPath: *policyPath,
		BillingPath: *billingPath, HistoryRoot: *historyRoot,
	})
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
	profile.AllowDeletes = strings.EqualFold(prompt(reader, "Allow separately gated destructive deletes for this profile? (yes/no)", "no"), "yes")
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

func optimizationPolicyInit(args []string) error {
	flags := flag.NewFlagSet("optimization policy init", flag.ContinueOnError)
	path := flags.String("path", "", "absolute optimization policy destination")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		defaultPath, err := optimization.DefaultPolicyPath()
		if err != nil {
			return err
		}
		*path = defaultPath
	}
	absPath, err := filepath.Abs(*path)
	if err != nil {
		return fmt.Errorf("resolve optimization policy path: %w", err)
	}
	reader := bufio.NewReader(os.Stdin)
	policy := optimization.Policy{
		Name:                   prompt(reader, "Policy name", "balanced-app"),
		Profile:                prompt(reader, "Apple Ads profile", "default"),
		AdAccountID:            prompt(reader, "Ad account ID", ""),
		PromotedObjectID:       prompt(reader, "App Adam ID", ""),
		CampaignIDs:            splitCommaValues(prompt(reader, "Campaign IDs (comma-separated, max 20)", "")),
		Mode:                   strings.ToLower(prompt(reader, "Mode (learning/active)", "learning")),
		MaxTotalDailyBudget:    appleads.Money{Amount: prompt(reader, "Maximum total daily budget", "100.00"), Currency: strings.ToUpper(prompt(reader, "Account currency", "USD"))},
		MaxCampaignDailyBudget: appleads.Money{Amount: prompt(reader, "Maximum per-campaign daily budget", "50.00"), Currency: ""},
		Permissions: optimization.Permissions{
			Budget:   yes(prompt(reader, "Allow budget actions? (yes/no)", "yes")),
			Bid:      yes(prompt(reader, "Allow bid actions? (yes/no)", "yes")),
			Strategy: yes(prompt(reader, "Allow bid-strategy actions? (yes/no)", "no")),
			Pause:    yes(prompt(reader, "Allow optimizer-owned pauses? (yes/no)", "yes")),
			Resume:   yes(prompt(reader, "Allow optimizer-owned resumes? (yes/no)", "no")),
			Retest:   yes(prompt(reader, "Allow bounded retests? (yes/no)", "no")),
		},
		Preset: "balanced",
	}
	policy.MaxCampaignDailyBudget.Currency = policy.MaxTotalDailyBudget.Currency
	if policy.Mode == "active" {
		policy.TargetInstallCPA = &appleads.Money{Amount: prompt(reader, "Target install CPA", ""), Currency: policy.MaxTotalDailyBudget.Currency}
	}
	if err := optimization.AddPolicy(absPath, policy); err != nil {
		return err
	}
	fmt.Printf("Optimization policy saved to %s\n", absPath)
	return nil
}

func optimizationPolicyValidate(args []string) error {
	flags := flag.NewFlagSet("optimization policy validate", flag.ContinueOnError)
	name := flags.String("name", "", "policy name")
	path := flags.String("path", "", "absolute optimization policy path")
	if err := flags.Parse(args); err != nil {
		return err
	}
	policies, source, err := optimization.LoadPolicies(*path)
	if err != nil {
		return err
	}
	policy, err := policies.Resolve(*name)
	if err != nil {
		return err
	}
	return printJSON(map[string]any{"status": "valid", "source": source, "name": policy.Name, "mode": policy.Mode, "campaignCount": len(policy.CampaignIDs)})
}

func optimizationDoctor(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("optimization doctor", flag.ContinueOnError)
	name := flags.String("policy", "", "policy name")
	policyPath := flags.String("optimization-policies", "", "absolute optimization policy path")
	configPath := flags.String("config", "", "absolute path to accounts.json")
	if err := flags.Parse(args); err != nil {
		return err
	}
	policies, _, err := optimization.LoadPolicies(*policyPath)
	if err != nil {
		return err
	}
	policy, err := policies.Resolve(*name)
	if err != nil {
		return err
	}
	cfg, source, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	profile, err := cfg.ResolveProfile(policy.Profile)
	if err != nil {
		return err
	}
	manager := appleads.NewManager(cfg, source)
	if _, err := manager.Do(ctx, profile.Name, "", appleads.Me()); err != nil {
		return err
	}
	if _, err := manager.Do(ctx, profile.Name, "", appleads.ACLs()); err != nil {
		return err
	}
	for _, campaignID := range policy.CampaignIDs {
		operation, _ := appleads.ResourceGet("campaigns", campaignID)
		if _, err := manager.Do(ctx, profile.Name, policy.AdAccountID, operation); err != nil {
			return fmt.Errorf("campaign %s: %w", campaignID, err)
		}
	}
	return printJSON(map[string]any{"status": "ready", "policy": policy.Name, "profile": policy.Profile, "adAccountId": policy.AdAccountID, "campaignCount": len(policy.CampaignIDs)})
}

func billingProfileInit(args []string) error {
	flags := flag.NewFlagSet("billing profile init", flag.ContinueOnError)
	path := flags.String("path", "", "absolute billing profile destination")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *path == "" {
		defaultPath, err := optimization.DefaultBillingPath()
		if err != nil {
			return err
		}
		*path = defaultPath
	}
	absPath, err := filepath.Abs(*path)
	if err != nil {
		return fmt.Errorf("resolve billing profile path: %w", err)
	}
	reader := bufio.NewReader(os.Stdin)
	profile := optimization.BillingProfile{
		Name:              prompt(reader, "Local billing profile name", "default"),
		PrimaryBuyerName:  prompt(reader, "Primary buyer name", ""),
		PrimaryBuyerEmail: prompt(reader, "Primary buyer email", ""),
		BillingEmail:      prompt(reader, "Billing email", ""),
		OrderNumber:       prompt(reader, "Order number (optional)", ""),
		ClientName:        prompt(reader, "Client name (optional)", ""),
	}
	if err := optimization.AddBillingProfile(absPath, profile); err != nil {
		return err
	}
	fmt.Printf("Billing profile saved to %s; private fields were not echoed\n", absPath)
	return nil
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
	return errors.New("usage: apple-ads-mcp {serve --stdio|config init|auth doctor|accounts discover|optimization policy init|optimization policy validate|optimization doctor|billing profile init|version}")
}

func splitCommaValues(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if value := strings.TrimSpace(part); value != "" {
			result = append(result, value)
		}
	}
	return result
}

func yes(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "yes")
}
