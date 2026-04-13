package new

// wizard.go — interactive TUI form for populating [CreateOptions] via huh.

import (
	"errors"
	"strings"

	"charm.land/huh/v2"

	"github.com/sciminds/cli/internal/cliui"
)

// RunWizard runs an interactive huh form to populate CreateOptions.
// Fields already set (e.g. from flags) are shown as pre-filled defaults.
func RunWizard(opts *CreateOptions) error {
	// Pre-fill author from git config
	if opts.AuthorName == "" {
		opts.AuthorName = gitConfigValue("user.name")
	}
	if opts.AuthorEmail == "" {
		opts.AuthorEmail = gitConfigValue("user.email")
	}

	// Default selects so they start on the right option
	if opts.PkgManager == "" {
		opts.PkgManager = "uv"
	}
	if opts.DocSystem == "" {
		opts.DocSystem = "myst"
	}

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Project name").
				Description("Lowercase, no spaces — becomes the directory name.").
				Placeholder("my-project").
				Value(&opts.Name).
				Validate(validateName),

			huh.NewSelect[string]().
				Title("Package manager").
				Options(
					huh.NewOption("uv     (pure Python, recommended)", "uv"),
					huh.NewOption("pixi   (conda, good for multi-language e.g. R & Python)", "pixi"),
				).
				Value(&opts.PkgManager),

			huh.NewSelect[string]().
				Title("Authoring system").
				Description("MyST uses .md notebooks. Quarto uses .qmd notebooks.").
				Options(
					huh.NewOption("MyST     (.md notebooks, recommended)", "myst"),
					huh.NewOption("Quarto   (.qmd notebooks)", "quarto"),
					huh.NewOption("None", "none"),
				).
				Value(&opts.DocSystem),

			huh.NewInput().
				Title("Author name").
				Placeholder("Your Name").
				Value(&opts.AuthorName),

			huh.NewInput().
				Title("Author email").
				Placeholder("you@example.com").
				Value(&opts.AuthorEmail),

			huh.NewInput().
				Title("Description").
				Description("Optional one-line project description.").
				Placeholder("").
				Value(&opts.Description),
		),
	).WithTheme(cliui.HuhTheme()).WithKeyMap(cliui.HuhKeyMap())

	return form.Run()
}

func validateName(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("project name cannot be empty")
	}
	if strings.ContainsAny(s, " \t\r\n") {
		return errors.New("project name must not contain spaces")
	}
	return nil
}
