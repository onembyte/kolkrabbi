package config

// ContinuitySettings is what a session does when the model behind it hits a
// limit (plan 35). Resume is auto (default) or manual: whether a paused
// session comes back on its own when the monitor sees the limit lifted, or
// waits for /resume. The other continuity keys (mode, select, preferred,
// order) arrive with V35.5 and supersede the routing.* pair.
type ContinuitySettings struct {
	Resume string `json:"resume,omitempty"`
}
