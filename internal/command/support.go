package command

import "github.com/spf13/cobra"

const (
	docsURL      = "https://docs.deplexo.com"
	communityURL = "https://dsc.gg/deplexo"
	supportEmail = "support@deplexo.com"
)

func (a *application) supportCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "support",
		Short: "Show email support, Discord community, and documentation links",
		Long:  "Find help with Deplexo. Use email for account-specific questions and Discord\nfor community support. Include the app UUID and CLI version when reporting an\nissue. Never share passwords, API keys, tokens, or environment secrets.\nThis command works offline and does not send a support request.",
		Args:  noArgs,
		RunE: func(*cobra.Command, []string) error {
			return a.details(map[string]string{
				"email": supportEmail, "discord": communityURL, "docs": docsURL,
			}, "Deplexo support", [][2]string{
				{"Email", supportEmail},
				{"Discord", communityURL},
				{"Docs", docsURL},
			})
		},
	}
}
