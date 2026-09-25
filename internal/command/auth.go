package command

import (
	"fmt"

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
			profile, err := manager.Login(cmd.Context(), scopes, func(device api.Device) error {
				if _, err := fmt.Fprintf(a.options.Err, "Open %s and enter %s.\n", output.Safe(device.VerificationURI), output.Safe(device.UserCode)); err != nil {
					return err
				}
				if !noBrowser && !a.noInput {
					if err := a.options.OpenBrowser(cmd.Context(), device.VerificationURI); err != nil {
						_, _ = fmt.Fprintln(a.options.Err, "Could not open a browser. Open the URL above to continue.")
					}
				}
				_, err := fmt.Fprintln(a.options.Err, "Waiting for approval...")
				return err
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
			return a.message(profile, profile.Email+" ("+profile.ID+")")
		}}
}
