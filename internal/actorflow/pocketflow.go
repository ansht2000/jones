package actorflow

type Action int

const (
	// Default action, noop
	Default Action = iota
)

type node interface {
	Prep()
	Exec(Action)
	Post(Action) Action
	Next(*node)
}

type fileReaderNode struct {
	parent   *node
	siblings []*node
}

func (fr *fileReaderNode) Prep() {}

func (fr *fileReaderNode) Exec(action Action) {}

func (fr *fileReaderNode) Post(action Action) Action { return Default }

func (fr *fileReaderNode) next(node *node) {}

func (fr *fileReaderNode) run() {
	fr.Prep()
	fr.Exec(Default)
	fr.Post(Default)
}
