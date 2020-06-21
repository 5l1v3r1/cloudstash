package common

type Metadata struct {
	Inode  int64
	Name   string
	URL    string
	Size   int64
	Mode   int
	Type   int
	Parent uint64
	NLink  int
}