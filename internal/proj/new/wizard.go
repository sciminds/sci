package new

// wizard.go — interactive TUI form for populating [CreateOptions] via huh.

import (
	"errors"
	"strings"

	"charm.land/huh/v2"
	"github.com/sciminds/cli/internal/uikit"
)

// RunWizard runs an interactive huh form to populate CreateOptions.
// Fields already set (e.g. from flags) are shown as pre-filled defaults.
// Selecting "writing" hides the package manager / doc system questions.
func RunWizard(opts *CreateOptions) error {
	// Pre-fill author from git config
	if opts.AuthorName == "" {
		opts.AuthorName = gitConfigValue("user.name")
	}
	if opts.AuthorEmail == "" {
		opts.AuthorEmail = gitConfigValue("user.email")
	}

	// Default selects so they start on the right option
	if opts.Kind == "" {
		opts.Kind = "python"
	}
	if opts.PkgManager == "" {
		opts.PkgManager = "uv"
	}
	if opts.DocSystem == "" {
		opts.DocSystem = "myst"
	}

	pythonOnly := func() bool { return opts.Kind != "python" }

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Project name").
				Description("Lowercase, no spaces — becomes the directory name.").
				Placeholder("my-project").
				Value(&opts.Name).
				Validate(validateName),

			huh.NewSelect[string]().
				Title("Project kind").
				Description("Python = data analysis (uv/pixi) with optional MyST/Quarto docs. Writing = pure manuscript (MyST → Typst → PDF).").
				Options(
					huh.NewOption("Python   (data analysis + writing)", "python"),
					huh.NewOption("Writing  (MyST → Typst PDF only)", "writing"),
				).
				Value(&opts.Kind),
		),

		huh.NewGroup(
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
		).WithHideFunc(pythonOnly),

		huh.NewGroup(
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
	)

	if err := uikit.RunForm(form); err != nil {
		return err
	}

	// Writing projects don't use these fields — clear so downstream logic
	// (Create + post-steps) sees the canonical empty state.
	if opts.Kind == "writing" {
		opts.PkgManager = ""
		opts.DocSystem = ""
	}
	return nil
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
