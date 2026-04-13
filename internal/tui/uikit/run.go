package uikit

import tea "charm.land/bubbletea/v2"

// Run launches a Bubbletea program and returns its error. It drains
// stdin after the program exits so leftover bytes don't leak into the
// shell. Use this for TUIs that don't need the final model state.
//
// Optional [tea.ProgramOption] values are forwarded to [tea.NewProgram]
// (e.g. tea.WithOutput(os.Stderr) for inline spinners).
//
//	if err := kit.Run(myModel); err != nil { … }
func Run(m tea.Model, opts ...tea.ProgramOption) error {
	p := tea.NewProgram(m, opts...)
	_, err := p.Run()
	DrainStdin()
	return err
}

// RunModel launches a Bubbletea program and returns the final model
// cast back to its concrete type. Use this when you need to inspect
// model state after the TUI exits (e.g. to read the user's selection).
//
//	final, err := kit.RunModel(myModel)
//	if err != nil { … }
//	fmt.Println(final.Chosen)
func RunModel[M tea.Model](m M, opts ...tea.ProgramOption) (M, error) {
	p := tea.NewProgram(m, opts...)
	final, err := p.Run()
	DrainStdin()
	if err != nil {
		var zero M
		return zero, err
	}
	return final.(M), nil
}
