package actorflow

type Action int

const (
	Default Action = iota
)

type Node interface {
	Prep()
	Exec()
	Post() Action
	Next() Node
}
