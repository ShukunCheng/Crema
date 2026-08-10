package agent

type Registry struct {
	Agents []Agent
}

func NewRegistry() *Registry {
	return &Registry{Agents: []Agent{NewClaude(), NewCodex(), NewMock()}}
}

// FirstAvailable returns the first installed agent, or nil if none are.
func (r *Registry) FirstAvailable() Agent {
	for _, a := range r.Agents {
		if a.Available() == nil {
			return a
		}
	}
	return nil
}

// Next cycles to the following available agent, skipping ones that aren't installed.
func (r *Registry) Next(cur string) Agent {
	n := len(r.Agents)
	if n == 0 {
		return nil
	}
	start := 0
	for i, a := range r.Agents {
		if a.Name() == cur {
			start = i
			break
		}
	}
	for off := 1; off <= n; off++ {
		a := r.Agents[(start+off)%n]
		if a.Available() == nil {
			return a
		}
	}
	return nil
}
