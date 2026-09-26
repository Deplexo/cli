package command

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Deplexo/cli/internal/api"
	"github.com/Deplexo/cli/internal/auth"
	"github.com/Deplexo/cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *application) authCommand() *cobra.Command {
	group := &cobra.Command{Use: "auth", Short: "Sign in, check your account, or sign out", Args: noArgs}
	var rawScopes string
	var readOnly, noBrowser bool
	login := &cobra.Command{Use: "login", Short: "Sign in with a pairing code in your browser", Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if cmd.Flags().Changed("scopes") && rawScopes == "" {
				return output.Usage("--scopes must include profile:read")
			}
			scopes, err := auth.Scopes(rawScopes, readOnly)
			if err != nil {
				return err
			}
			manager, err := a.manager()
			if err != nil {
				return err
			}
			profile, err := manager.Login(cmd.Context(), scopes, func(ctx context.Context, device api.Device) error {
				return a.pair(ctx, device, noBrowser)
			})
			if err != nil {
				return err
			}
			return a.message(profile, "Signed in as "+profile.Email+".")
		}}
	login.Flags().StringVar(&rawScopes, "scopes", "", "Scopes to request, separated by spaces or commas; replaces the defaults")
	login.Flags().BoolVar(&readOnly, "read-only", false, "Request read access to your profile, apps, and logs")
	login.Flags().BoolVar(&noBrowser, "no-browser", false, "Print pairing instructions without opening a browser")
	group.AddCommand(login)
	status := a.whoamiCommand()
	status.Use, status.Short = "status", "Check your current sign-in with the API"
	group.AddCommand(status, &cobra.Command{Use: "logout", Short: "Sign out and remove the saved credentials for this profile", Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := a.manager()
			if err != nil {
				return err
			}
			if err := manager.Logout(cmd.Context()); err != nil {
				return err
			}
			return a.message(struct {
				SignedOut bool `json:"signed_out"`
			}{true}, "Signed out.")
		}})
	return group
}

func (a *application) pair(ctx context.Context, device api.Device, noBrowser bool) error {
	if err := a.printer(a.options.Err).Fields("Sign in to Deplexo", [][2]string{{"Open", device.VerificationURI}, {"Pairing code", device.UserCode}}); err != nil {
		return err
	}
	if !noBrowser && !a.noInput && a.options.IsTerminal() {
		if _, err := fmt.Fprint(a.options.Err, "Please press Enter to open your browser, or type n and press Enter to open the link yourself: "); err != nil {
			return err
		}
		open, err := confirmBrowser(ctx, a.options.In)
		if err != nil {
			return err
		}
		if open {
			if err := a.options.OpenBrowser(ctx, device.VerificationURI); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if _, err := fmt.Fprintln(a.options.Err, "Could not open a browser. Open the URL above to continue."); err != nil {
					return err
				}
			}
		}
	}
	_, err := fmt.Fprintln(a.options.Err, "Waiting for approval...")
	return err
}

func confirmBrowser(ctx context.Context, input io.Reader) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	type response struct {
		line string
		err  error
	}
	answer := make(chan response, 1)
	// A terminal read cannot be cancelled portably without closing the caller's
	// stdin. The process exits on cancellation; the buffered send cannot block.
	go func() {
		line, err := bufio.NewReader(io.LimitReader(input, 1024)).ReadString('\n')
		answer <- response{line, err}
	}()
	select {
	case <-ctx.Done():
		return false, ctx.Err()
	case result := <-answer:
		if err := ctx.Err(); err != nil {
			return false, err
		}
		if errors.Is(result.err, io.EOF) {
			return false, nil
		}
		if result.err != nil {
			return false, errors.New("could not read your answer; use --no-browser to sign in manually")
		}
		line := strings.TrimSuffix(strings.TrimSuffix(result.line, "\n"), "\r")
		if line == "" {
			return true, nil
		}
		if strings.EqualFold(line, "n") {
			return false, nil
		}
		return false, output.Usage("press Enter to open the browser, or use --no-browser to sign in manually")
	}
}

func (a *application) whoamiCommand() *cobra.Command {
	return &cobra.Command{Use: "whoami", Short: "Show the account used by this token or sign-in", Args: noArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			manager, err := a.manager()
			if err != nil {
				return err
			}
			token, err := manager.Token(cmd.Context(), "profile:read")
			if err != nil {
				return err
			}
			profile, err := manager.API.Profile(cmd.Context(), token)
			if err != nil {
				return err
			}
			return a.details(profile, "Signed-in account", [][2]string{{"Email", profile.Email}, {"Account", profile.ID}})
		}}
}
