package slash

func RegisterRestart(r *Registry) {
	r.Register(Command{Name: "restart", Help: "Restart this TUI only, with confirmation. Windows: manual restart instructions.", Handler: func(args []string) Result {
		if len(args) != 0 {
			return Result{Kind: ResultText, Text: "Usage: /restart — restarts this terminal client, not the agent."}
		}
		return Result{Kind: ResultRestartTUI}
	}})
}
