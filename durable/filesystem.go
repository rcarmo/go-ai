package durable

import "context"

// FileError adapts the pinned filesystem Result error to Go's error return.
// Code is portable; Cause is process-local and must not be persisted by tools.
type FileError struct {
	Code  string
	Path  string
	Cause error
}

func (e *FileError) Error() string { return "durable: filesystem " + e.Code }
func (e *FileError) Unwrap() error { return e.Cause }

type FileInfo struct {
	Name    string
	Path    string
	Kind    string // file, directory or symlink
	Size    int64
	MtimeMs int64
}
type TextLine struct {
	Text       string
	Terminated bool
}
type BinaryReader interface {
	Info(context.Context) (FileInfo, error)
	Read(context.Context, int64, int) ([]byte, error)
	ScanLines(context.Context, int64, *int64) (LineScan, error)
	Close(context.Context) error
}
type LineScan struct {
	Newlines                      int64
	Start, End                    int64
	FirstLineEnd, LastLineStart   int64
	SelectedBytes, FirstLineBytes int64
}
type DirPage struct {
	Entries []FileInfo
	Done    bool
}
type DirReader interface {
	Next(context.Context, int) (DirPage, error)
	Close(context.Context) error
}
type WatchTarget struct {
	Path          string
	Recursive     bool
	ExcludeHidden bool
	ExcludeNames  []string
}
type WatchChange struct {
	Paths    []string
	Overflow bool
	Error    *FileError
}
type FileWatcher interface {
	Mode() string
	Close(context.Context) error
}

// ExtendedFileSystem adds 1.0.4 capabilities without breaking existing injected
// environments. Production reads prefer this bounded positional interface.
type ExtendedFileSystem interface {
	FileSystem
	OpenBinaryReader(context.Context, string, bool) (BinaryReader, error)
	OpenDirReader(context.Context, string) (DirReader, error)
	Watch(context.Context, []WatchTarget, func(WatchChange)) (FileWatcher, error)
}

type TextLineReader interface {
	ReadLine(context.Context) (TextLine, bool, error)
	Close(context.Context) error
}

// FileSystem is the native counterpart of the pinned portable filesystem.
// Equal IDs share one path namespace, independently of Cwd. Temporary paths
// belong to callers; Cleanup does not remove them.
type FileSystem interface {
	ExecutionEnvironment
	ID() string
	AbsolutePath(context.Context, string) (string, error)
	JoinPath(context.Context, ...string) (string, error)
	ReadTextFile(context.Context, string) (string, error)
	OpenTextLineReader(context.Context, string) (TextLineReader, error)
	ReadTextLines(context.Context, string, *int) ([]string, error)
	ReadBinaryFile(context.Context, string) ([]byte, error)
	WriteFile(context.Context, string, []byte) error
	AppendFile(context.Context, string, []byte) error
	TruncateFile(context.Context, string, int64) error
	FlushFile(context.Context, string) error
	RenameFile(context.Context, string, string) error
	FileInfo(context.Context, string) (FileInfo, error)
	ListDir(context.Context, string) ([]FileInfo, error)
	CanonicalPath(context.Context, string) (string, error)
	Exists(context.Context, string) (bool, error)
	CreateDir(context.Context, string, bool) error
	Remove(context.Context, string, bool, bool) error
	CreateTempDir(context.Context, string) (string, error)
	CreateTempFile(context.Context, string, string) (string, error)
	Cleanup(context.Context) error
}
