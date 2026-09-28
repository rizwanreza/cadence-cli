package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rizwanreza/cadence-cli/internal/api"
	"github.com/rizwanreza/cadence-cli/internal/config"
	"github.com/spf13/cobra"
)

func newLoginCmd(a *App) *cobra.Command {
	var noBrowser bool
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Save your API token (validated before it's saved)",
		Long: `Sign in by pasting a personal API token from Settings → CLI Access
(<host>/settings#cli). The token is read with hidden input, checked against
the server, then saved to ~/.config/cadence/config.json (mode 0600).

Non-interactive: pipe the token on stdin (echo "$TOKEN" | cadence login) or
pass --token (it lands in shell history, so prefer stdin). --url picks and
saves the host. For one-off commands you can skip login and set CADENCE_TOKEN.`,
		Example: `  cadence login
  cadence login --url http://localhost:3000
  printf '%s' "$CADENCE_TOKEN" | cadence login --json`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			host, err := config.ResolveHost(a.urlFlag, cfg)
			if err != nil {
				return usageErrorf("%v", err)
			}
			token := strings.TrimSpace(a.tokenFlag)
			interactive := a.StdinIsTTY != nil && a.StdinIsTTY()
			if token == "" {
				settingsURL := host + "/settings#cli"
				if interactive {
					fmt.Fprintf(a.Err, "Create a token at %s (Settings → CLI Access).\n", settingsURL)
					if !noBrowser && a.OpenBrowser != nil && a.confirm("Open it in your browser?", true) {
						if err := a.OpenBrowser(settingsURL); err != nil {
							fmt.Fprintf(a.Err, "Couldn't open a browser (%v). Visit the link above.\n", err)
						}
					}
					fmt.Fprint(a.Err, "Paste your token (input is hidden): ")
					token, err = a.ReadSecret()
					fmt.Fprintln(a.Err)
				} else {
					token, err = readLine(a.In)
				}
				if err != nil {
					return err
				}
				token = strings.TrimSpace(token)
			}
			if token == "" {
				return &UsageError{Msg: "no token provided", Hint: "Create one at " + host + "/settings#cli, then run `cadence login` again."}
			}

			c, err := a.clientFor(host, token)
			if err != nil {
				return err
			}
			me, err := c.Me(a.ctx())
			if err != nil {
				var apiErr *api.Error
				if errors.As(err, &apiErr) && (apiErr.Status == 401 || apiErr.Status == 403) {
					return &exitError{code: ExitAuth, err: fmt.Errorf("%s rejected that token; nothing was saved. Create a new one at %s/settings#cli", host, host)}
				}
				return fmt.Errorf("could not verify the token, so nothing was saved: %w", err)
			}

			cfg.Host = host
			if host == config.DefaultHost {
				cfg.Host = "" // follow the default if it ever moves
			}
			cfg.Token = token
			cfg.Email = me.User.Email
			cfg.TimeZone = me.User.TimeZone
			if err := config.Save(cfg); err != nil {
				return err
			}
			path, _ := config.Path()
			if a.jsonOut {
				return a.printJSON(map[string]any{"signed_in": true, "host": host, "user": me.User, "today": me.Today, "config_path": path})
			}
			a.printf("%s\n", accentStyle.Render(fmt.Sprintf("Signed in as %s (%s).", me.User.Email, me.User.TimeZone)))
			a.printf("Host:   %s\nConfig: %s\n", host, path)
			a.println("Next: `cadence status`, and `cadence skill install` to teach your agents.")
			return nil
		},
	}
	cmd.Flags().BoolVar(&noBrowser, "no-browser", false, "Don't offer to open the settings page")
	return cmd
}

func newLogoutCmd(a *App) *cobra.Command {
	var revoke bool
	cmd := &cobra.Command{
		Use:   "logout",
		Short: "Forget the saved token (--revoke also invalidates it on the server)",
		Example: `  cadence logout
  cadence logout --revoke`,
		Args: noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			revoked := false
			var revokeErr error
			if revoke {
				c, err := a.authedClient()
				if err != nil {
					return err
				}
				revokeErr = c.RevokeToken(a.ctx())
				if revokeErr != nil && !api.IsStatus(revokeErr, 401) {
					return fmt.Errorf("could not revoke the token (it is still saved locally): %w", revokeErr)
				}
				revoked = revokeErr == nil
			}
			hadToken := cfg.Token != ""
			cfg.Token = ""
			cfg.Email = ""
			cfg.TimeZone = ""
			if err := config.Save(cfg); err != nil {
				return err
			}
			a.cfg = &cfg
			if a.jsonOut {
				return a.printJSON(map[string]any{"signed_out": true, "had_saved_token": hadToken, "revoked": revoked})
			}
			switch {
			case revoked:
				a.println("Revoked the token on the server and removed it from this machine.")
			case revoke:
				a.println("The server had already rejected this token. Removed it from this machine.")
			case hadToken:
				a.println("Removed the saved token. It still works elsewhere until you revoke it (cadence logout --revoke, or Settings → CLI Access).")
			default:
				a.println("No saved token to remove.")
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&revoke, "revoke", false, "Also revoke the token on the server")
	return cmd
}

func newAuthCmd(a *App) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "auth",
		Short: "Inspect the saved login",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Show the host, signed-in user, token prefix and last use",
		Args:  noArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := a.config()
			if err != nil {
				return err
			}
			host, err := config.ResolveHost(a.urlFlag, cfg)
			if err != nil {
				return usageErrorf("%v", err)
			}
			token, source := config.ResolveToken(a.tokenFlag, cfg)
			out := map[string]any{"host": host, "signed_in": false, "token_source": string(source)}
			if token == "" {
				if a.jsonOut {
					_ = a.printJSON(out)
					return &exitError{code: ExitAuth, err: errors.New("not signed in")}
				}
				return &exitError{code: ExitAuth, err: fmt.Errorf("not signed in to %s; run `cadence login`", host)}
			}
			out["token_prefix"] = config.TokenPrefix(token)
			c, err := a.clientFor(host, token)
			if err != nil {
				return err
			}
			me, err := c.Me(a.ctx())
			if err != nil {
				return err
			}
			out["signed_in"] = true
			out["user"] = me.User
			out["today"] = me.Today
			if me.Token != nil {
				out["token_prefix"] = me.Token.Prefix
				out["token_last_used_at"] = me.Token.LastUsedAt
			}
			if a.jsonOut {
				return a.printJSON(out)
			}
			a.printf("Host:      %s\n", host)
			a.printf("Signed in: %s (%s)\n", me.User.Email, me.User.TimeZone)
			a.printf("Token:     %v (from %s)\n", out["token_prefix"], source)
			if me.Token != nil && me.Token.LastUsedAt != nil {
				a.printf("Last used: %s\n", *me.Token.LastUsedAt)
			}
			return nil
		},
	})
	return cmd
}
