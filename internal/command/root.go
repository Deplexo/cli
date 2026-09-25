package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/Deplexo/cli/internal/api"
	"github.com/Deplexo/cli/internal/auth"
	"github.com/Deplexo/cli/internal/config"
	"github.com/Deplexo/cli/internal/credentials"
	"github.com/Deplexo/cli/internal/output"
	"github.com/spf13/cobra"
)

type Options struct {
	In              io.Reader
	Out, Err        io.Writer
	Version, Commit string
	LookupEnv       func(string) (string, bool)
	ConfigDir       func() (string, error)
	WorkingDir      func() (string, error)
	Transport       http.RoundTripper
	OpenBrowser     func(context.Context, string) error
}

type application struct {
	options                 Options
	origin, profile         string
	insecure, noInput, json bool
	started                 bool
}

func Execute(ctx context.Context, options Options, args []string) int {
	root, app := newRoot(options)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	if err != nil {
		if !app.started {
			err = output.Usage(err.Error())
		}
		output.Report(app.options.Err, err, app.json)
	}
	return output.ExitCode(err)
}

func newRoot(options Options) (*cobra.Command, *application) {
	if options.In == nil {
		options.In = os.Stdin
	}
	if options.Out == nil {
		options.Out = os.Stdout
	}
	if options.Err == nil {
		options.Err = os.Stderr
	}
	if options.LookupEnv == nil {
		options.LookupEnv = os.LookupEnv
	}
	if options.ConfigDir == nil {
		options.ConfigDir = os.UserConfigDir
	}
	if options.WorkingDir == nil {
		options.WorkingDir = os.Getwd
	}
	if options.OpenBrowser == nil {
		options.OpenBrowser = openBrowser
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	a := &application{options: options}
	root := &cobra.Command{Use: "deplexo", Short: "Manage Deplexo apps from your terminal", SilenceErrors: true, SilenceUsage: true,
		PersistentPreRun: func(*cobra.Command, []string) { a.started = true },
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return output.Usage("unknown command; run `deplexo --help`")
			}
			return cmd.Help()
		},
	}
	root.SetIn(options.In)
	root.SetOut(options.Out)
	root.SetErr(options.Err)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return output.Usage(err.Error()) })
	root.PersistentFlags().StringVar(&a.origin, "origin", "", "HTTPS API origin (DEPLEXO_ORIGIN)")
	root.PersistentFlags().StringVar(&a.profile, "profile", "", "Credential profile (DEPLEXO_PROFILE)")
	root.PersistentFlags().BoolVar(&a.insecure, "insecure-storage", false, "Use a plaintext credential file instead of the OS keyring")
	root.PersistentFlags().BoolVar(&a.noInput, "no-input", false, "Disable prompts and automatic browser opening")
	root.PersistentFlags().BoolVar(&a.json, "json", false, "Write JSON results; followed logs use JSONL")
	root.AddCommand(a.authCommand(), a.whoamiCommand(), a.appsCommand(), a.linkCommand(), a.unlinkCommand(), a.deploymentsCommand(), a.logsCommand())
	root.AddCommand(&cobra.Command{Use: "version", Short: "Print the CLI version", Args: noArgs,
		RunE: func(*cobra.Command, []string) error {
			result := struct {
				Version string `json:"version"`
				Commit  string `json:"commit"`
				OS      string `json:"os"`
				Arch    string `json:"arch"`
			}{options.Version, options.Commit, runtime.GOOS, runtime.GOARCH}
			if a.json {
				return output.JSON(options.Out, result)
			}
			_, err := fmt.Fprintln(options.Out, "deplexo "+output.Safe(options.Version)+" ("+runtime.GOOS+"/"+runtime.GOARCH+")")
			return err
		}})
	return root, a
}

func noArgs(cmd *cobra.Command, args []string) error {
	if err := cobra.NoArgs(cmd, args); err != nil {
		return output.Usage(err.Error())
	}
	return nil
}

func oneArg(cmd *cobra.Command, args []string) error {
	if err := cobra.ExactArgs(1)(cmd, args); err != nil {
		return output.Usage(err.Error())
	}
	return nil
}

var profilePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}$`)

func (a *application) manager() (*auth.Manager, error) {
	base, err := a.options.ConfigDir()
	if err != nil {
		return nil, errors.New("could not locate the user configuration directory")
	}
	directory := filepath.Join(base, "deplexo")
	settings, err := config.LoadSettings(directory)
	if err != nil {
		return nil, err
	}
	selectValue := func(flag, env, setting, fallback string) string {
		if flag != "" {
			return flag
		}
		if value, ok := a.options.LookupEnv(env); ok && value != "" {
			return value
		}
		if setting != "" {
			return setting
		}
		return fallback
	}
	origin := selectValue(a.origin, "DEPLEXO_ORIGIN", settings.Origin, api.DefaultOrigin)
	profile := selectValue(a.profile, "DEPLEXO_PROFILE", settings.Profile, "default")
	if !profilePattern.MatchString(profile) {
		return nil, output.Usage("profile must contain 1 to 64 letters, digits, underscores, or hyphens and start with a letter or digit")
	}
	client, err := api.New(origin, a.options.Transport)
	if err != nil {
		return nil, output.Usage(err.Error())
	}
	token, tokenSet := a.options.LookupEnv("DEPLEXO_TOKEN")
	return &auth.Manager{API: client, Store: &credentials.Store{Directory: filepath.Join(directory, "credentials"), Origin: client.Origin(), Profile: profile, Insecure: a.insecure, NoInput: a.noInput}, Profile: profile, TokenEnv: token, TokenEnvSet: tokenSet}, nil
}

func (a *application) selectedApp(flag string) (string, error) {
	if flag != "" {
		if !api.ValidID(flag) {
			return "", output.Usage("--app must be an app UUID")
		}
		return strings.ToLower(flag), nil
	}
	dir, err := a.options.WorkingDir()
	if err != nil {
		return "", errors.New("could not locate the current directory")
	}
	project, err := config.LoadProject(dir)
	if err != nil {
		return "", output.Usage(err.Error())
	}
	return strings.ToLower(project.App), nil
}

func (a *application) message(value any, text string) error {
	if a.json {
		return output.JSON(a.options.Out, value)
	}
	_, err := fmt.Fprintln(a.options.Out, output.Safe(text))
	return err
}

func (a *application) logText(text string) error {
	for _, line := range strings.Split(strings.TrimSuffix(text, "\n"), "\n") {
		if _, err := fmt.Fprintln(a.options.Out, output.Safe(line)); err != nil {
			return err
		}
	}
	return nil
}

func openBrowser(ctx context.Context, url string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.CommandContext(ctx, "/usr/bin/open", url)
	case "windows":
		cmd = exec.CommandContext(ctx, "rundll32.exe", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.CommandContext(ctx, "xdg-open", url)
	}
	return cmd.Run()
}
