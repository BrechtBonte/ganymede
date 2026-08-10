package topology

// Focus moves keyboard focus in the dock to the working client's pane — the
// move alt+g makes when it goes from the Dashboard to the working client,
// given automatically once a jump or an open has already put something new
// in front of you.
func (h Harness) Focus() error {
	return h.dock().run("select-pane", "-t", "="+DockSession+":0.1")
}
