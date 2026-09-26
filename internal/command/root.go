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
	"golang.org/x/term"
)

type Options struct {
	In               io.Reader
	Out, Err         io.Writer
	Version, Commit  string
	LookupEnv        func(string) (string, bool)
	ConfigDir        func() (string, error)
	WorkingDir       func() (string, error)
	Transport        http.RoundTripper
	UpdateTransport  http.RoundTripper
	Executable       func() (string, error)
	OpenBrowser      func(context.Context, string) error
	IsTerminal       func() bool
	IsOutputTerminal func(io.Writer) bool
}

type application struct {
	options                 Options
	origin, profile         string
	color                   string
	insecure, noInput, json bool
	started                 bool
}

func Execute(ctx context.Context, options Options, args []string) int {
	root, app := newRoot(options)
	root.SetArgs(args)
	command, err := root.ExecuteContextC(ctx)
	if err != nil {
		if !app.started {
			err = output.Usage(err.Error())
		}
		app.printer(app.options.Err).Report(err, app.json)
	} else {
		app.notifyUpdate(ctx, command)
		if ctx.Err() != nil {
			app.printer(app.options.Err).Report(ctx.Err(), app.json)
			return output.ExitCode(ctx.Err())
		}
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
	if options.Executable == nil {
		options.Executable = os.Executable
	}
	if options.OpenBrowser == nil {
		options.OpenBrowser = openBrowser
	}
	if options.IsTerminal == nil {
		options.IsTerminal = func() bool {
			file, ok := options.In.(*os.File)
			return ok && term.IsTerminal(int(file.Fd()))
		}
	}
	if options.IsOutputTerminal == nil {
		options.IsOutputTerminal = func(w io.Writer) bool { file, ok := w.(*os.File); return ok && term.IsTerminal(int(file.Fd())) }
	}
	if options.Version == "" {
		options.Version = "dev"
	}
	a := &application{options: options}
	root := &cobra.Command{Use: "deplexo", Short: "Create and manage Deplexo apps from your terminal", SilenceErrors: true, SilenceUsage: true,
		PersistentPreRunE: func(*cobra.Command, []string) error {
			a.started = true
			if a.color != "auto" && a.color != "always" && a.color != "never" {
				return output.Usage("--color must be auto, always, or never")
			}
			return nil
		},
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
	defaultHelp := root.HelpFunc()
	root.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		var text strings.Builder
		writer := cmd.OutOrStdout()
		cmd.SetOut(&text)
		defaultHelp(cmd, args)
		cmd.SetOut(writer)
		_ = a.printer(writer).Help(text.String())
	})
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return output.Usage(err.Error()) })
	root.PersistentFlags().StringVar(&a.origin, "origin", "", "HTTPS API origin (DEPLEXO_ORIGIN)")
	root.PersistentFlags().StringVar(&a.profile, "profile", "", "Sign-in profile (DEPLEXO_PROFILE)")
	root.PersistentFlags().BoolVar(&a.insecure, "insecure-storage", false, "Use a plaintext credential file instead of the OS keyring")
	root.PersistentFlags().BoolVar(&a.noInput, "no-input", false, "Disable prompts and browser opening")
	root.PersistentFlags().BoolVar(&a.json, "json", false, "Write JSON; use one object per line with logs --follow")
	root.PersistentFlags().StringVar(&a.color, "color", "auto", "Terminal colors: auto, always, or never (respects NO_COLOR)")
	root.AddGroup(&cobra.Group{ID: "apps", Title: "Apps and deployments:"}, &cobra.Group{ID: "account", Title: "Account:"}, &cobra.Group{ID: "other", Title: "Other commands:"})
	root.SetHelpCommandGroupID("other")
	root.SetCompletionCommandGroupID("other")
	root.AddCommand(a.authCommand(), a.whoamiCommand(), a.appsCommand(), a.deployCommand(), a.linkCommand(), a.unlinkCommand(), a.deploymentsCommand(), a.logsCommand(), a.upgradeCommand())
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
	// Cobra's command lookup needs to know --help is boolean before parsing
	// later flags such as --color always.
	root.InitDefaultHelpFlag()
	for _, command := range root.Commands() {
		command.InitDefaultHelpFlag()
		switch command.Name() {
		case "auth", "whoami":
			command.GroupID = "account"
		case "version", "upgrade":
			command.GroupID = "other"
		default:
			command.GroupID = "apps"
		}
	}
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
		return nil, errors.New("could not find your configuration directory")
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
		return "", errors.New("could not find the current directory")
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
	return a.printer(a.options.Out).Message(text)
}

func (a *application) printer(w io.Writer) *output.Printer {
	mode := a.color
	if a.json {
		mode = "never"
	}
	return output.NewPrinter(w, mode, a.options.LookupEnv)
}

func (a *application) details(value any, title string, fields [][2]string) error {
	if a.json {
		return output.JSON(a.options.Out, value)
	}
	return a.printer(a.options.Out).Fields(title, fields)
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
