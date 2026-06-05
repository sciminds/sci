package new

// wizard.go — interactive TUI form for populating [CreateOptions] via the
// uikit multi-field form builder.

import (
	"errors"
	"strings"

	"github.com/sciminds/cli/internal/uikit"
)

// RunWizard runs an interactive form to populate CreateOptions.
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
	if opts.MdLayout == "" {
		opts.MdLayout = "single-file"
	}

	// Template select needs three states: lab / default / other (free-text).
	// Map the pre-fill into the right state and capture any custom name.
	templateChoice := "lab"
	templateCustom := ""
	switch opts.Template {
	case "", "lab":
		templateChoice = "lab"
	case "default":
		templateChoice = "default"
	default:
		templateChoice = "custom"
		templateCustom = opts.Template
	}

	pythonOnly := func() bool { return opts.Kind != "python" }
	noManuscript := func() bool {
		if opts.Kind == "writing" {
			return false
		}
		return opts.Kind != "python" || opts.DocSystem != "myst"
	}

	form := uikit.NewForm(
		uikit.FormGroup(
			uikit.FormInput(&opts.Name, "Project name",
				uikit.WithDescription("Lowercase, no spaces — becomes the directory name."),
				uikit.WithPlaceholder("my-project"),
				uikit.WithValidation(validateName)),
			uikit.FormSelect(&opts.Kind, "Project kind",
				[]uikit.Option[string]{
					uikit.NewOption("Python   (data analysis + writing)", "python"),
					uikit.NewOption("Writing  (MyST → Typst PDF only)", "writing"),
				},
				uikit.WithDescription("Python = data analysis (uv/pixi) with optional MyST/Quarto docs. Writing = pure manuscript (MyST → Typst → PDF).")),
		),
		uikit.FormGroup(
			uikit.FormSelect(&opts.PkgManager, "Package manager",
				[]uikit.Option[string]{
					uikit.NewOption("uv     (pure Python, recommended)", "uv"),
					uikit.NewOption("pixi   (conda, good for multi-language e.g. R & Python)", "pixi"),
				}),
			uikit.FormSelect(&opts.DocSystem, "Authoring system",
				[]uikit.Option[string]{
					uikit.NewOption("MyST     (.md notebooks, recommended)", "myst"),
					uikit.NewOption("Quarto   (.qmd notebooks)", "quarto"),
					uikit.NewOption("None", "none"),
				},
				uikit.WithDescription("MyST uses .md notebooks. Quarto uses .qmd notebooks.")),
		).HideWhen(pythonOnly),
		uikit.FormGroup(
			uikit.FormSelect(&opts.MdLayout, "Manuscript layout",
				[]uikit.Option[string]{
					uikit.NewOption("single-file (recommended for solo authoring)", "single-file"),
					uikit.NewOption("composed    (separate sections/ files)", "composed"),
				},
				uikit.WithDescription("Single-file = abstract/keypoints/etc. inline in main.md frontmatter. Composed = separate sections/ files.")),
			uikit.FormSelect(&templateChoice, "Typst template",
				[]uikit.Option[string]{
					uikit.NewOption("lab     (local, editable)", "lab"),
					uikit.NewOption("default (MyST built-in default)", "default"),
					uikit.NewOption("other   (any MyST template name)", "custom"),
				},
				uikit.WithDescription("`lab` ships a local, editable copy of the sci-preprint template under _templates/paper/. `default` and any other name use a MyST-hosted template.")),
		).HideWhen(noManuscript),
		uikit.FormGroup(
			uikit.FormInput(&templateCustom, "Template name",
				uikit.WithDescription("Any MyST-resolvable template, e.g. lapreprint-typst, arxiv-two-column."),
				uikit.WithPlaceholder("lapreprint-typst"),
				uikit.WithValidation(validateTemplateName)),
		).HideWhen(func() bool {
			return noManuscript() || templateChoice != "custom"
		}),
		uikit.FormGroup(
			uikit.FormInput(&opts.AuthorName, "Author name",
				uikit.WithPlaceholder("Your Name")),
			uikit.FormInput(&opts.AuthorEmail, "Author email",
				uikit.WithPlaceholder("you@example.com")),
			uikit.FormInput(&opts.Description, "Description",
				uikit.WithDescription("Optional one-line project description.")),
		),
	)

	if err := form.Run(); err != nil {
		return err
	}

	// Resolve Template choice into the canonical opts.Template value.
	switch templateChoice {
	case "custom":
		opts.Template = strings.TrimSpace(templateCustom)
	default:
		opts.Template = templateChoice
	}

	// Writing projects don't use these fields — clear so downstream logic
	// (Create + post-steps) sees the canonical empty state.
	if opts.Kind == "writing" {
		opts.PkgManager = ""
		opts.DocSystem = ""
	}
	// Clear manuscript fields for non-manuscript paths so they don't leak
	// into TemplateVars where they're meaningless.
	if opts.Kind == "python" && opts.DocSystem != "myst" {
		opts.MdLayout = ""
		opts.Template = ""
	}
	return nil
}

func validateTemplateName(s string) error {
	if strings.TrimSpace(s) == "" {
		return errors.New("template name cannot be empty")
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
