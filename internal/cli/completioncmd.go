package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
)

// shellCompletion describes how one shell finds a generated script. Persistence
// differs per shell, and no process can read an interactive shell's completion
// state, so each entry also carries the check the user runs in that shell.
type shellCompletion struct {
	name     string
	file     string
	generate func(*cobra.Command, io.Writer, bool) error
	// directory is the default install target: a user-owned path outside any
	// package manager prefix, so the script survives npm, nvm and brew changes.
	directory func() (string, error)
	// loading explains how the shell picks up an installed file.
	loading func(path string) string
	check   string
	long    string
}

const completionPersistence = `Install it PERSISTENTLY for your user (Homebrew and APT already do this; npm,
npx and manual installs do not):

	csquad completion install --shell %[1]s

That writes the script to a directory you own and prints any line you must add
yourself; it never edits your shell configuration. Use 'csquad completion
status' to see what is currently on disk.`

func completionShells() []shellCompletion {
	return []shellCompletion{
		{
			name: "bash",
			file: "csquad",
			generate: func(root *cobra.Command, w io.Writer, descriptions bool) error {
				return root.GenBashCompletionV2(w, descriptions)
			},
			directory: func() (string, error) { return dataDirectory("bash-completion", "completions") },
			loading: func(path string) string {
				// The generated script calls bash-completion helpers such as
				// _get_comp_words_by_ref, so that package is a hard requirement.
				return "bash-completion v2 reads that directory automatically and provides the\n" +
					"helpers this script needs; start a new shell.\n" +
					"If nothing completes, add this to ~/.bashrc after bash-completion loads:\n\n\tsource " + quoteShell(path)
			},
			check: "complete -p csquad",
			long: "Generate the bash completion script for csquad.\n\nLoad it into the CURRENT shell only:\n\n\tsource <(csquad completion bash)\n\n" +
				fmt.Sprintf(completionPersistence, "bash"),
		},
		{
			name: "zsh",
			file: "_csquad",
			generate: func(root *cobra.Command, w io.Writer, descriptions bool) error {
				if descriptions {
					return root.GenZshCompletion(w)
				}
				return root.GenZshCompletionNoDesc(w)
			},
			directory: func() (string, error) { return dataDirectory("zsh", "site-functions") },
			loading: func(path string) string {
				return "Run this line in the CURRENT zsh, then add the same line at the END of\n~/.zshrc (after Oh My Zsh or other completion setup) for future shells:\n\n\t" +
					"(( $+functions[compdef] )) || { autoload -Uz compinit; compinit; }; source " + quoteShell(path) +
					"\n\nThis initializes completion if needed and registers csquad directly, even with\nan older compinit cache. No fpath edit or cache deletion is required.\nInstalling the file alone cannot update an already-open shell."
			},
			check: "print -r -- ${_comps[csquad]:-missing}",
			long: "Generate the zsh completion script for csquad.\n\nLoad it into the CURRENT shell only:\n\n\tsource <(csquad completion zsh)\n\n" +
				fmt.Sprintf(completionPersistence, "zsh"),
		},
		{
			name: "fish",
			file: "csquad.fish",
			generate: func(root *cobra.Command, w io.Writer, descriptions bool) error {
				return root.GenFishCompletion(w, descriptions)
			},
			directory: func() (string, error) { return configDirectory("fish", "completions") },
			loading: func(string) string {
				return "fish reads that directory automatically; start a new shell."
			},
			check: "complete -c csquad",
			long: "Generate the fish completion script for csquad.\n\nLoad it into the CURRENT shell only:\n\n\tcsquad completion fish | source\n\n" +
				fmt.Sprintf(completionPersistence, "fish"),
		},
		{
			name: "powershell",
			file: "csquad.ps1",
			generate: func(root *cobra.Command, w io.Writer, descriptions bool) error {
				if descriptions {
					return root.GenPowerShellCompletionWithDesc(w)
				}
				return root.GenPowerShellCompletion(w)
			},
			// PowerShell has no autoloaded completion directory, so the script
			// goes next to other csquad data and the profile sources it.
			directory: func() (string, error) { return dataDirectory("csquad") },
			loading: func(path string) string {
				return "Add this line to your profile ($PROFILE), then start a new shell:\n\n\t. " + quotePowerShell(path)
			},
			check: "Get-Command csquad",
			long: "Generate the powershell completion script for csquad.\n\nLoad it into the CURRENT session only:\n\n\tcsquad completion powershell | Out-String | Invoke-Expression\n\n" +
				fmt.Sprintf(completionPersistence, "powershell"),
		},
	}
}

// quoteShell renders a path as one Bourne/Zsh word. Every printed line is meant
// to be pasted into a startup file, where an unquoted space would silently
// become two fpath entries and leave completion dead with no error.
func quoteShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

// quotePowerShell does the same for a PowerShell single-quoted string, which
// escapes an embedded quote by doubling it instead.
func quotePowerShell(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func dataDirectory(parts ...string) (string, error) {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate the completion directory: %w", err)
		}
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(append([]string{base}, parts...)...), nil
}

func configDirectory(parts ...string) (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("locate the completion directory: %w", err)
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(append([]string{base}, parts...)...), nil
}

// selectShell resolves --shell, falling back to the login shell in $SHELL.
func selectShell(name string) (shellCompletion, error) {
	shells := completionShells()
	names := make([]string, 0, len(shells))
	for _, shell := range shells {
		names = append(names, shell.name)
	}
	if name == "" {
		name = filepath.Base(os.Getenv("SHELL"))
	}
	for _, shell := range shells {
		if shell.name == name {
			return shell, nil
		}
	}
	return shellCompletion{}, fmt.Errorf("set --shell to one of %s; $SHELL did not name a supported shell", strings.Join(names, ", "))
}

func (s shellCompletion) script(root *cobra.Command, descriptions bool) ([]byte, error) {
	var buffer bytes.Buffer
	if err := s.generate(root, &buffer, descriptions); err != nil {
		return nil, fmt.Errorf("generate the %s completion script: %w", s.name, err)
	}
	return buffer.Bytes(), nil
}

func (s shellCompletion) target(directory string) (string, error) {
	if directory == "" {
		resolved, err := s.directory()
		if err != nil {
			return "", err
		}
		directory = resolved
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return "", fmt.Errorf("resolve %q: %w", directory, err)
	}
	return filepath.Join(absolute, s.file), nil
}

// install writes the script only when the content differs, so repeated runs
// leave the file and its timestamp untouched.
func install(path string, script []byte) (string, error) {
	existing, err := os.ReadFile(path)
	switch {
	case err == nil && bytes.Equal(existing, script):
		return "Unchanged", nil
	case err != nil && !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	verb := "Installed"
	if err == nil {
		verb = "Updated"
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", fmt.Errorf("create %s: %w", filepath.Dir(path), err)
	}
	// Write through a temporary file: a shell must never autoload a partial script.
	staged := path + ".csquad-install"
	if err := os.WriteFile(staged, script, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", staged, err)
	}
	if err := os.Rename(staged, path); err != nil {
		_ = os.Remove(staged)
		return "", fmt.Errorf("replace %s: %w", path, err)
	}
	return verb, nil
}

func completionCommand(root *cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use:   "completion",
		Short: "Generate, install, or inspect shell completion",
		Long: "Generate a completion script, install it for your user, or report what is\ninstalled. Homebrew and APT packages install completion for you; npm, npx and\nmanual installs need one explicit command.\n\n" +
			"\tcsquad completion install          Persistent, for your login shell\n" +
			"\tcsquad completion status           What is on disk, and how to verify it\n" +
			"\tsource <(csquad completion zsh)    Current shell only",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE:              func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	for _, shell := range completionShells() {
		generate := &cobra.Command{
			Use:               shell.name,
			Short:             "Generate the completion script for " + shell.name,
			Long:              shell.long,
			Args:              cobra.NoArgs,
			ValidArgsFunction: cobra.NoFileCompletions,
			RunE: func(c *cobra.Command, _ []string) error {
				descriptions, err := c.Flags().GetBool("no-descriptions")
				if err != nil {
					return err
				}
				script, err := shell.script(root, !descriptions)
				if err != nil {
					return err
				}
				_, err = c.OutOrStdout().Write(script)
				return err
			},
		}
		generate.Flags().Bool("no-descriptions", false, "disable completion descriptions")
		command.AddCommand(generate)
	}
	command.AddCommand(completionInstallCommand(root), completionStatusCommand(root))
	return command
}

func completionInstallCommand(root *cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use:   "install",
		Short: "Install the completion script into a directory you own",
		Long: "Write the completion script for one shell into a user-owned directory and\nprint the remaining setup, which you apply yourself. This command never edits\n~/.zshrc, ~/.bashrc or any other shell configuration, and rewrites the script\nonly when its content changed, so it is safe to repeat.\n\n" +
			"The installed script locates csquad on PATH at completion time, so it keeps\nworking across npm upgrades and nvm Node switches.\n\n" +
			"\tcsquad completion install\n\tcsquad completion install --shell zsh\n\tcsquad completion install --shell zsh --dir ~/.zsh/completions",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(c *cobra.Command, _ []string) error {
			name, err := c.Flags().GetString("shell")
			if err != nil {
				return err
			}
			shell, err := selectShell(name)
			if err != nil {
				return &usageError{cause: err, command: c.CommandPath()}
			}
			descriptions, err := c.Flags().GetBool("no-descriptions")
			if err != nil {
				return err
			}
			directory, err := c.Flags().GetString("dir")
			if err != nil {
				return err
			}
			// Filesystem failures are operational, not usage errors.
			markExecuted(c)
			script, err := shell.script(root, !descriptions)
			if err != nil {
				return err
			}
			path, err := shell.target(directory)
			if err != nil {
				return err
			}
			verb, err := install(path, script)
			if err != nil {
				return err
			}
			out := c.OutOrStdout()
			if _, err := fmt.Fprintf(out, "%s %s completion: %s\n\n%s\n\nVerify inside that shell: %s\n", verb, shell.name, path, shell.loading(path), shell.check); err != nil {
				return err
			}
			return nil
		},
	}
	command.Flags().String("shell", "", "Shell to install for (defaults to $SHELL)")
	command.Flags().String("dir", "", "Directory to install into (defaults to a user data directory)")
	command.Flags().Bool("no-descriptions", false, "disable completion descriptions")
	if err := command.RegisterFlagCompletionFunc("shell", completionShellNames()); err != nil {
		panic(err)
	}
	if err := command.RegisterFlagCompletionFunc("dir", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return nil, cobra.ShellCompDirectiveFilterDirs
	}); err != nil {
		panic(err)
	}
	if err := command.RegisterFlagCompletionFunc("no-descriptions", cobra.NoFileCompletions); err != nil {
		panic(err)
	}
	return command
}

func completionStatusCommand(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:               "status",
		Short:             "Report installed completion files and how to verify them",
		Long:              "Report, for every supported shell, where 'csquad completion install' puts the\nscript and whether that file matches this binary.\n\nThis inspects files only. A shell exports neither its fpath nor its loaded\ncompletion functions, so whether completion is actually active is visible only\ninside that shell: run the printed check there.",
		Args:              cobra.NoArgs,
		ValidArgsFunction: cobra.NoFileCompletions,
		RunE: func(c *cobra.Command, _ []string) error {
			markExecuted(c)
			out := c.OutOrStdout()
			if _, err := fmt.Fprintln(out, "Completion files for this binary (file state only; run each check in that shell):"); err != nil {
				return err
			}
			for _, shell := range completionShells() {
				path, err := shell.target("")
				if err != nil {
					return err
				}
				script, err := shell.script(root, true)
				if err != nil {
					return err
				}
				state := "missing"
				switch existing, err := os.ReadFile(path); {
				case err == nil && bytes.Equal(existing, script):
					state = "current"
				case err == nil:
					state = "differs"
				case !errors.Is(err, fs.ErrNotExist):
					return fmt.Errorf("read %s: %w", path, err)
				}
				if _, err := fmt.Fprintf(out, "\n%-11s %-8s %s\n  check: %s\n", shell.name, state, path, shell.check); err != nil {
					return err
				}
			}
			if _, err := fmt.Fprintln(out, "\n'differs' only compares content with this binary's default output; installing\nwith --no-descriptions or an older csquad is a normal cause.\nRun 'csquad completion install' to create or refresh a file."); err != nil {
				return err
			}
			return nil
		},
	}
}

func completionShellNames() func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	names := []string{}
	for _, shell := range completionShells() {
		names = append(names, shell.name)
	}
	return cobra.FixedCompletions(names, cobra.ShellCompDirectiveNoFileComp)
}

// markExecuted keeps failures after this point out of the usage-error path.
func markExecuted(cmd *cobra.Command) {
	if cmd.Annotations == nil {
		cmd.Annotations = map[string]string{}
	}
	cmd.Annotations["executed"] = "true"
}
