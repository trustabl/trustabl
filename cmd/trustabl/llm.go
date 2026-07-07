package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/trustabl/trustabl/internal/llm"
)

func newLLMCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "llm",
		Short: "Manage LLM provider configuration",
		Long: `Manage the optional LLM provider configuration (provider, model, and API key)
used for bring-your-own-key enrichment. Configuration is stored in your user
config directory with 0600 permissions.

NOTE: LLM enrichment is not wired into the scan yet. These commands only store
the provider, model, and key for that future path — no scan makes an LLM call
today, with or without a key configured.`,
		Example: `  # One-time setup
  trustabl llm provider set anthropic
  trustabl llm key set
  trustabl llm list`,
	}
	cmd.AddCommand(newLLMListCommand())
	cmd.AddCommand(newLLMKeyCommand())
	cmd.AddCommand(newLLMModelCommand())
	cmd.AddCommand(newLLMProviderCommand())
	return cmd
}

// ── list ─────────────────────────────────────────────────────────────────────

func newLLMListCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show current LLM configuration",
		Long: `Show the configured providers, their models, and masked API keys. The active
provider is marked with an asterisk (*).`,
		Example: "  trustabl llm list",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLLMList(cmd)
		},
	}
}

func runLLMList(cmd *cobra.Command) error {
	if !llm.Exists() {
		fmt.Fprintln(cmd.OutOrStdout(),
			"No LLM configuration found. Run `trustabl llm key set` to get started.")
		return nil
	}
	cfg, err := llm.Load()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PROVIDER\tMODEL\tKEY")
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		p := cfg.Providers[name]
		active := ""
		if name == cfg.Active {
			active = " *"
		}
		fmt.Fprintf(w, "%s%s\t%s\t%s\n", name, active, p.Model, llm.MaskKey(p.Key))
	}
	return w.Flush()
}

// ── key ──────────────────────────────────────────────────────────────────────

func newLLMKeyCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "key",
		Short: "Manage the API key for the active provider",
		Long: `Manage the API key for the active provider (set, get, delete). Keys are stored in
your user config directory with 0600 permissions and are shown masked.`,
		Example: `  trustabl llm key set
  trustabl llm key get
  trustabl llm key delete`,
	}
	cmd.AddCommand(newLLMKeySetCommand())
	cmd.AddCommand(newLLMKeyGetCommand())
	cmd.AddCommand(newLLMKeyDeleteCommand())
	return cmd
}

func newLLMKeySetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set [key]",
		Short: "Store the API key for the active provider",
		Long: `Store the API key for the active provider. Pass the key as an argument, or omit
it to be prompted (the key is read from the terminal without echo). The key is
validated against the active provider's expected format before it is saved.`,
		Example: `  # Prompt for the key (not echoed)
  trustabl llm key set

  # Pass the key directly (note: it may be saved in shell history)
  trustabl llm key set sk-ant-...`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLLMKeySet(cmd, args)
		},
	}
}

func runLLMKeySet(cmd *cobra.Command, args []string) error {
	var key string
	if len(args) == 1 {
		key = strings.TrimSpace(args[0])
	} else {
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return fmt.Errorf("stdin is not a terminal; pass the key as an argument: trustabl llm key set <key>")
		}
		fmt.Fprint(os.Stderr, "Enter API key: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return fmt.Errorf("reading API key: %w", err)
		}
		key = strings.TrimSpace(string(b))
	}
	cfg, err := llm.Load()
	if err != nil {
		return err
	}
	if err := llm.ValidateKey(cfg.Active, key); err != nil {
		return err
	}
	cfg.SetKey(key)
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "API key for %s saved.\n", cfg.Active)
	return nil
}

func newLLMKeyGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "get",
		Short:   "Show the stored API key (masked)",
		Long:    "Show the stored API key for the active provider, masked (only a few trailing\ncharacters are revealed).",
		Example: "  trustabl llm key get",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLLMKeyGet(cmd)
		},
	}
}

func runLLMKeyGet(cmd *cobra.Command) error {
	cfg, err := llm.Load()
	if err != nil {
		return err
	}
	p := cfg.ActiveProvider()
	if p.Key == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "No API key configured for %s.\n", cfg.Active)
		return nil
	}
	fmt.Fprintln(cmd.OutOrStdout(), llm.MaskKey(p.Key))
	return nil
}

func newLLMKeyDeleteCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "delete",
		Short:   "Delete the stored API key for the active provider",
		Long:    "Delete the stored API key for the active provider. You are asked to confirm\nbefore the key is removed.",
		Example: "  trustabl llm key delete",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLLMKeyDelete(cmd)
		},
	}
}

func runLLMKeyDelete(cmd *cobra.Command) error {
	cfg, err := llm.Load()
	if err != nil {
		return err
	}
	p := cfg.ActiveProvider()
	if p.Key == "" {
		fmt.Fprintf(cmd.OutOrStdout(), "No API key configured for %s.\n", cfg.Active)
		return nil
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "Delete API key for %s? [y/N]: ", cfg.Active)
	var response string
	fmt.Fscanln(cmd.InOrStdin(), &response)
	if strings.ToLower(strings.TrimSpace(response)) != "y" {
		fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
		return nil
	}
	cfg.ClearKey()
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "API key for %s deleted.\n", cfg.Active)
	return nil
}

// ── model ─────────────────────────────────────────────────────────────────────

func newLLMModelCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "model",
		Short:   "Manage the model for the active provider",
		Long:    "Manage the model used for the active provider.",
		Example: "  trustabl llm model set claude-sonnet-4",
	}
	cmd.AddCommand(newLLMModelSetCommand())
	return cmd
}

func newLLMModelSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "set <model>",
		Short:   "Set the model for the active provider",
		Long:    "Set the model identifier for the active provider (e.g. a Claude or GPT model\nname). The value is stored as-is and is not validated against the provider.",
		Example: "  trustabl llm model set claude-sonnet-4",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLLMModelSet(cmd, args[0])
		},
	}
}

func runLLMModelSet(cmd *cobra.Command, model string) error {
	cfg, err := llm.Load()
	if err != nil {
		return err
	}
	cfg.SetModel(model)
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Model for %s set to %s.\n", cfg.Active, model)
	return nil
}

// ── provider ──────────────────────────────────────────────────────────────────

func newLLMProviderCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "provider",
		Short: "Manage LLM providers",
		Long: `Manage which LLM provider is active and list the providers you have configured.
Setting a provider that has no key yet prompts you to run "trustabl llm key set".`,
		Example: `  trustabl llm provider list
  trustabl llm provider set anthropic`,
	}
	cmd.AddCommand(newLLMProviderSetCommand())
	cmd.AddCommand(newLLMProviderListCommand())
	return cmd
}

func newLLMProviderSetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "set <provider>",
		Short: "Set the active LLM provider",
		Long: `Set the active LLM provider. The provider must be one Trustabl knows about; an
unknown name is rejected with the list of known providers. If the chosen provider
has no API key yet, you are reminded to run "trustabl llm key set".`,
		Example: "  trustabl llm provider set anthropic",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runLLMProviderSet(cmd, args[0])
		},
	}
}

func runLLMProviderSet(cmd *cobra.Command, provider string) error {
	if !llm.IsKnownProvider(provider) {
		return fmt.Errorf("unknown provider %q (known providers: %s)",
			provider, strings.Join(llm.KnownProviders(), ", "))
	}
	cfg, err := llm.Load()
	if err != nil {
		return err
	}
	_, existed := cfg.Providers[provider]
	cfg.SetActive(provider)
	if err := cfg.Save(); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Active provider set to %s.\n", provider)
	if !existed {
		fmt.Fprintf(cmd.OutOrStdout(),
			"API key for %s is not set. Run `trustabl llm key set` to configure it.\n", provider)
	}
	return nil
}

func newLLMProviderListCommand() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Short:   "List configured LLM providers",
		Long:    "List the providers you have configured. The active provider is marked with an\nasterisk (*).",
		Example: "  trustabl llm provider list",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runLLMProviderList(cmd)
		},
	}
}

func runLLMProviderList(cmd *cobra.Command) error {
	if !llm.Exists() {
		fmt.Fprintln(cmd.OutOrStdout(),
			"No LLM configuration found. Run `trustabl llm key set` to get started.")
		return nil
	}
	cfg, err := llm.Load()
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "PROVIDER")
	names := make([]string, 0, len(cfg.Providers))
	for name := range cfg.Providers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		active := ""
		if name == cfg.Active {
			active = " *"
		}
		fmt.Fprintf(w, "%s%s\n", name, active)
	}
	return w.Flush()
}
