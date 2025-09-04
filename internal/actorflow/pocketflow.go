package actorflow

type Action int

const (
	Default Action = iota
)

type Node interface {
	Prep()
	Exec()
	Post() Action
	Next(Node)
}

type FileNode struct {
	FileContent string
	
}

func (fn *FileNode) Prep() {}

func (fn *FileNode) Exec() {}

func (fn *FileNode) Post() Action { return Default }

func (fn *FileNode) Next(node Node) {}

