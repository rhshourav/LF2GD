package core

type Segment struct {
	Index int
	Start int64
	End   int64
}

type FileJob struct {
	Name       string
	URL        string
	Size       int64
	Segments   []Segment
	Downloaded int64
}
