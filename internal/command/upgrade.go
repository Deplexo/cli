package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Deplexo/cli/internal/config"
	"github.com/Deplexo/cli/internal/output"
	"github.com/Deplexo/cli/internal/update"
	"github.com/Deplexo/cli/internal/version"
	"github.com/spf13/cobra"
)

func (a *application) upgradeCommand() *cobra.Command {
	var yes, check bool
	cmd := &cobra.Command{Use: "upgrade", Short: "Check for a newer CLI release and install it", Args: noArgs, RunE: func(cmd *cobra.Command, _ []string) error {
		if !version.ValidTag(a.options.Version) {
			return output.Usage("development builds cannot upgrade themselves; install a release from https://cli.deplexo.com")
		}
		ctx, cancel := context.WithTimeout(cmd.Context(), 3*time.Minute)
		defer cancel()
		client := update.New(a.options.UpdateTransport)
		tag, err := client.Latest(ctx)
		if err != nil {
			return err
		}
		available := version.Newer(tag, a.options.Version)
		if check || !available {
			return a.details(struct {
				Current   string `json:"current"`
				Latest    string `json:"latest"`
				Available bool   `json:"available"`
			}{a.options.Version, tag, available}, "CLI update", [][2]string{{"Installed", a.options.Version}, {"Available", tag}})
		}
		if !yes {
			if a.noInput || a.json || !a.options.IsTerminal() {
				return output.Usage("a newer release is available; pass --yes to upgrade without a prompt")
			}
			proceed, err := a.confirmUpgrade(ctx, tag)
			if err != nil || !proceed {
				return err
			}
		}
		return a.installUpgrade(ctx, client, tag)
	}}
	cmd.Flags().BoolVar(&yes, "yes", false, "Install the newer release without prompting")
	cmd.Flags().BoolVar(&check, "check", false, "Show the available version without installing")
	return cmd
}

func (a *application) confirmUpgrade(ctx context.Context, tag string) (bool, error) {
	if _, err := fmt.Fprintf(a.options.Err, "Deplexo %s is available (installed: %s). Upgrade now? [y/N] ", output.Safe(tag), output.Safe(a.options.Version)); err != nil {
		return false, err
	}
	answer, err := readAnswer(ctx, a.options.In)
	if errors.Is(err, io.EOF) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return answer == "y" || answer == "Y" || answer == "yes" || answer == "YES", nil
}

func (a *application) installUpgrade(ctx context.Context, client *update.Client, tag string) error {
	target, err := a.options.Executable()
	if err != nil {
		return errors.New("could not locate this executable; rerun the installer")
	}
	if _, err := fmt.Fprintf(a.options.Err, "Downloading and verifying Deplexo %s...\n", output.Safe(tag)); err != nil {
		return err
	}
	if err := client.Install(ctx, tag, a.options.Version, target); err != nil {
		return err
	}
	return a.message(struct {
		Version string `json:"version"`
	}{tag}, "Updated Deplexo to "+tag+". Your next command will use the new version.")
}

func (a *application) notifyUpdate(ctx context.Context, cmd *cobra.Command) {
	if cmd == cmd.Root() {
		return
	}
	if !a.started || a.noInput || a.json || !version.ValidTag(a.options.Version) || !a.options.IsTerminal() {
		return
	}
	for c := cmd; c != nil; c = c.Parent() {
		if c.Name() == "help" || c.Name() == "version" || c.Name() == "completion" || c.Name() == "upgrade" || c.Name() == "__complete" || c.Name() == "__completeNoDesc" {
			return
		}
	}
	if !a.options.IsOutputTerminal(a.options.Err) {
		return
	}
	if !a.options.IsOutputTerminal(a.options.Out) {
		return
	}
	if value, _ := a.options.LookupEnv("DEPLEXO_NO_UPDATE_CHECK"); value != "" {
		return
	}
	if value, _ := a.options.LookupEnv("CI"); value != "" {
		return
	}
	base, err := a.options.ConfigDir()
	if err != nil {
		return
	}
	directory := filepath.Join(base, "deplexo")
	state, err := config.LoadUpdateState(directory)
	if err != nil && !os.IsNotExist(err) {
		return
	}
	now := time.Now()
	if now.Before(state.RemindAt) {
		return
	}
	client := update.New(a.options.UpdateTransport)
	fresh := false
	if now.Sub(state.CheckedAt) >= 24*time.Hour || state.CheckedAt.After(now) {
		state.CheckedAt = now
		state.Latest = ""
		// Cache failed checks too, so an offline terminal is not retried on every command.
		if config.SaveUpdateState(directory, state) != nil {
			return
		}
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		tag, err := client.Latest(checkCtx)
		cancel()
		if err != nil {
			return
		}
		state.Latest = tag
		fresh = true
		if config.SaveUpdateState(directory, state) != nil {
			return
		}
	}
	if !version.Newer(state.Latest, a.options.Version) {
		return
	}
	// Refresh before consent, so the confirmed version is exactly the one installed.
	tag := state.Latest
	if !fresh {
		checkCtx, checkCancel := context.WithTimeout(ctx, 2*time.Second)
		tag, err = client.Latest(checkCtx)
		checkCancel()
		if err != nil {
			state.Latest = ""
			state.CheckedAt = time.Now()
			_ = config.SaveUpdateState(directory, state)
			return
		}
		state.Latest = tag
		state.CheckedAt = time.Now()
		_ = config.SaveUpdateState(directory, state)
		if !version.Newer(tag, a.options.Version) {
			return
		}
	}
	proceed, err := a.confirmUpgrade(ctx, tag)
	state.RemindAt = time.Now().Add(24 * time.Hour)
	_ = config.SaveUpdateState(directory, state)
	if err != nil || !proceed {
		return
	}
	updateCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// Automatic update messages belong on stderr with the prompt.
	previousOut := a.options.Out
	a.options.Out = a.options.Err
	err = a.installUpgrade(updateCtx, client, tag)
	a.options.Out = previousOut
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		a.printer(a.options.Err).Error("Upgrade failed: " + err.Error() + ". Your command completed before the upgrade attempt.")
	}
}
