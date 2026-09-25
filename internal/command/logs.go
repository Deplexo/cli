package command

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Deplexo/cli/internal/api"
	"github.com/Deplexo/cli/internal/auth"
	"github.com/Deplexo/cli/internal/output"
	"github.com/spf13/cobra"
)

func (a *application) deploymentsCommand() *cobra.Command {
	group := &cobra.Command{Use: "deployments", Short: "Show deployment history and build logs", Args: noArgs}
	var appFlag string
	var limit, offset int
	list := &cobra.Command{Use: "list", Short: "List an app's deployments", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 200 || offset < 0 {
			return output.Usage("--limit must be 1 to 200 and --offset must be zero or greater")
		}
		id, err := a.selectedApp(appFlag)
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
		result, err := manager.API.Deployments(cmd.Context(), token, id, limit, offset)
		if err != nil {
			return err
		}
		if a.json {
			return output.JSON(a.options.Out, result)
		}
		for _, deployment := range result.Data {
			if _, err := fmt.Fprintf(a.options.Out, "%s  %s  %s\n", output.Safe(deployment.ID), output.Safe(deployment.Status), output.Safe(deployment.CreatedAt)); err != nil {
				return err
			}
		}
		return nil
	}}
	list.Flags().StringVar(&appFlag, "app", "", "App UUID; defaults to .deplexo.json")
	list.Flags().IntVar(&limit, "limit", 50, "Number of deployments to return (1 to 200)")
	list.Flags().IntVar(&offset, "offset", 0, "Number of deployments to skip")
	logs := &cobra.Command{Use: "logs <deployment-uuid>", Short: "Show a deployment's build log snapshot", Args: oneArg, RunE: func(cmd *cobra.Command, args []string) error {
		if !api.ValidID(args[0]) {
			return output.Usage("deployment ID must be a UUID")
		}
		manager, err := a.manager()
		if err != nil {
			return err
		}
		token, err := manager.Token(cmd.Context(), "logs:read")
		if err != nil {
			return err
		}
		result, err := manager.API.DeploymentLogs(cmd.Context(), token, strings.ToLower(args[0]))
		if err != nil {
			return err
		}
		if a.json {
			return output.JSON(a.options.Out, result)
		}
		return a.logText(result.BuildLogs)
	}}
	group.AddCommand(list, logs)
	return group
}

func (a *application) logsCommand() *cobra.Command {
	var appFlag, since string
	var follow bool
	var limit int
	var timeout time.Duration
	command := &cobra.Command{Use: "logs", Short: "Show runtime logs and follow new lines with --follow", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if limit < 1 || limit > 1000 || timeout <= 0 {
			return output.Usage("--limit must be 1 to 1000 and --timeout must be positive")
		}
		id, err := a.selectedApp(appFlag)
		if err != nil {
			return err
		}
		manager, err := a.manager()
		if err != nil {
			return err
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
		defer cancel()
		seen := make(map[int64]struct{})
		var order []int64
		cursor := since
		for {
			token, err := manager.Token(ctx, "logs:read")
			if err != nil {
				return err
			}
			result, err := manager.API.RuntimeLogs(ctx, token, id, cursor, limit)
			if err != nil {
				var remote *api.Error
				if follow && errors.As(err, &remote) && remote.Status == 429 {
					if err := auth.Sleep(ctx, max(3*time.Second, remote.RetryAfter)); err != nil {
						return err
					}
					continue
				}
				return err
			}
			if !follow && a.json {
				return output.JSON(a.options.Out, result)
			}
			for _, line := range result.Lines {
				if _, exists := seen[line.ID]; line.ID != 0 && exists {
					continue
				}
				if line.ID != 0 {
					seen[line.ID] = struct{}{}
					order = append(order, line.ID)
				}
				if a.json {
					err = output.JSON(a.options.Out, line)
				} else {
					err = a.logText(line.Message)
				}
				if err != nil {
					return err
				}
			}
			for len(order) > 10000 {
				delete(seen, order[0])
				order = order[1:]
			}
			if !follow {
				return nil
			}
			if result.NextSince == "" {
				return errors.New("API response is missing the resume cursor; log following stopped")
			}
			cursor = result.NextSince
			if err := auth.Sleep(ctx, 3*time.Second); err != nil {
				return err
			}
		}
	}}
	command.Flags().StringVar(&appFlag, "app", "", "App UUID; defaults to .deplexo.json")
	command.Flags().StringVar(&since, "since", "", "Resume cursor (nextSince) from an earlier log response")
	command.Flags().BoolVar(&follow, "follow", false, "Poll for new runtime logs using the server cursor")
	command.Flags().IntVar(&limit, "limit", 500, "Maximum lines per request (1 to 1000)")
	command.Flags().DurationVar(&timeout, "timeout", 30*time.Minute, "Maximum time to follow logs")
	return command
}
