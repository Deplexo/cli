package command

import (
	"net/url"
	"strings"

	"github.com/Deplexo/cli/internal/api"
	"github.com/Deplexo/cli/internal/config"
	"github.com/Deplexo/cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *application) appsCommand() *cobra.Command {
	group := &cobra.Command{Use: "apps", Short: "Inspect, create, start, stop, or delete an app", Args: noArgs}
	var appID string
	get := &cobra.Command{Use: "get", Short: "Show app details", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		id, err := a.selectedApp(appID)
		if err != nil {
			return err
		}
		manager, err := a.manager()
		if err != nil {
			return err
		}
		token, err := manager.Token(cmd.Context(), "app:read")
		if err != nil {
			return err
		}
		result, err := manager.API.App(cmd.Context(), token, id)
		if err != nil {
			return err
		}
		return a.message(result, result.App.Name+" ("+result.App.ID+"): "+result.App.Status)
	}}
	get.Flags().StringVar(&appID, "app", "", "App UUID; defaults to .deplexo.json")
	group.AddCommand(get, a.createCommand())
	for _, action := range []struct {
		name, short, scope string
		confirm            bool
	}{
		{"start", "Start a stopped app without rebuilding", "app:start", false},
		{"stop", "Stop a running app", "app:stop", true},
		{"cancel", "Cancel an app's in-progress deployment", "app:deploy", true},
		{"delete", "Delete an app and its resources", "app:delete", true},
	} {
		var idFlag string
		var yes bool
		command := &cobra.Command{Use: action.name, Short: action.short, Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := a.selectedApp(idFlag)
			if err != nil {
				return err
			}
			if action.confirm && !yes {
				return output.Usage("check the app UUID, then pass --yes to confirm")
			}
			manager, err := a.manager()
			if err != nil {
				return err
			}
			token, err := manager.Token(cmd.Context(), action.scope)
			if err != nil {
				return err
			}
			if action.name == "cancel" {
				result, err := manager.API.CancelDeployment(cmd.Context(), token, id)
				if err != nil {
					return err
				}
				return a.message(result, "Cancelled the current deployment for app "+id+".")
			}
			result, err := manager.API.AppAction(cmd.Context(), token, id, action.name)
			if err != nil {
				return err
			}
			return a.message(result, "App "+id+": "+result.Status)
		}}
		command.Flags().StringVar(&idFlag, "app", "", "App UUID; defaults to .deplexo.json")
		if action.confirm {
			command.Flags().BoolVar(&yes, "yes", false, "Confirm this operation")
		}
		group.AddCommand(command)
	}
	return group
}

func (a *application) createCommand() *cobra.Command {
	var request api.CreateApp
	command := &cobra.Command{Use: "create", Short: "Create an app and its first deployment from a Git repository", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		u, err := url.Parse(request.RepoURL)
		if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path == "" {
			return output.Usage("--repo must be an HTTPS repository URL without credentials or a query")
		}
		if strings.TrimSpace(request.Name) == "" {
			return output.Usage("--name is required")
		}
		manager, err := a.manager()
		if err != nil {
			return err
		}
		token, err := manager.Token(cmd.Context(), "app:deploy")
		if err != nil {
			return err
		}
		result, err := manager.API.CreateApp(cmd.Context(), token, request)
		if err != nil {
			return err
		}
		return a.message(result, "Created app "+result.AppID+"; deployment "+result.DeploymentID+" is "+result.Status+".")
	}}
	command.Flags().StringVar(&request.Name, "name", "", "Name for the new app")
	command.Flags().StringVar(&request.RepoURL, "repo", "", "HTTPS URL of the Git repository")
	command.Flags().StringVar(&request.Framework, "framework", "", "Build framework; omit to detect it automatically")
	command.Flags().StringVar(&request.RootDir, "root-dir", "", "Source subdirectory to build")
	return command
}

func (a *application) linkCommand() *cobra.Command {
	var idFlag string
	command := &cobra.Command{Use: "link", Short: "Link this directory to an existing app", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if idFlag == "" {
			return output.Usage("--app is required")
		}
		id, err := a.selectedApp(idFlag)
		if err != nil {
			return err
		}
		manager, err := a.manager()
		if err != nil {
			return err
		}
		token, err := manager.Token(cmd.Context(), "app:read")
		if err != nil {
			return err
		}
		if _, err := manager.API.App(cmd.Context(), token, id); err != nil {
			return err
		}
		dir, err := a.options.WorkingDir()
		if err != nil {
			return err
		}
		if err := config.Link(dir, id); err != nil {
			return err
		}
		return a.message(config.Project{Version: 1, App: id}, "Linked this directory to app "+id+".")
	}}
	command.Flags().StringVar(&idFlag, "app", "", "App UUID to link to this directory")
	return command
}

func (a *application) unlinkCommand() *cobra.Command {
	return &cobra.Command{Use: "unlink", Short: "Remove this directory's link to an app", Args: noArgs, RunE: func(*cobra.Command, []string) error {
		dir, err := a.options.WorkingDir()
		if err != nil {
			return err
		}
		if err := config.Unlink(dir); err != nil {
			return err
		}
		return a.message(struct {
			Unlinked bool `json:"unlinked"`
		}{true}, "Removed the link to the app.")
	}}
}
