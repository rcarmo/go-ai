package tools

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rcarmo/go-ai/durable"
)

func TestBinaryReader104IdentityNoFollowAndLineScan(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	env := LocalEnv(dir)
	raw := append([]byte{0xef, 0xbb, 0xbf}, []byte("first\n€\nlast\n")...)
	if err := os.WriteFile(filepath.Join(dir, "file"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	reader, err := env.OpenBinaryReader(ctx, "file", false)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(ctx)
	end := int64(2)
	scan, err := reader.ScanLines(ctx, 0, &end)
	if err != nil || scan.Newlines != 3 || scan.Start != 0 || scan.End != 12 || scan.SelectedBytes != 9 || scan.FirstLineBytes != 5 {
		t.Fatal(scan, err)
	}
	if err := os.Rename(filepath.Join(dir, "file"), filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := reader.Read(ctx, 0, len(raw))
	if err != nil || !bytes.Equal(got, raw) {
		t.Fatal("reader followed replaced path", got, err)
	}
	if err := os.Symlink("old", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	if opened, err := env.OpenBinaryReader(ctx, "link", true); err == nil {
		opened.Close(ctx)
		t.Fatal("noFollow accepted symlink")
	}
	for _, data := range [][]byte{{0xf0, 0x9f, 0x98}, {0xe2, 0x82}, {0xff}, {0xe0, 0x80, 0x80}} {
		size := utf8Size{}
		for _, b := range data {
			size.add([]byte{b}, false)
		}
		size.add(nil, true)
		want := int64(3)
		if data[0] == 0xe0 {
			want = 9
		}
		if size.bytes != want {
			t.Fatal("TextDecoder replacement size", data, size.bytes, want)
		}
	}
}

func TestDirReader104PagesAndPollingWatchLifecycle(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dir := t.TempDir()
	env := LocalEnvWithWatchOptions(dir, WatchOptions{PollInterval: 50 * time.Millisecond})
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(name), 0600); err != nil {
			t.Fatal(err)
		}
	}
	reader, err := env.OpenDirReader(ctx, ".")
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close(context.Background())
	names := map[string]bool{}
	for {
		page, err := reader.Next(ctx, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Entries) > 2 {
			t.Fatal(page)
		}
		for _, entry := range page.Entries {
			if names[entry.Name] {
				t.Fatal("duplicate page", entry)
			}
			names[entry.Name] = true
		}
		if page.Done {
			break
		}
	}
	if len(names) != 3 {
		t.Fatal(names)
	}
	changes := make(chan durable.WatchChange, 8)
	watcher, err := env.Watch(ctx, []durable.WatchTarget{{Path: ".", Recursive: true, ExcludeHidden: true}}, func(change durable.WatchChange) { changes <- change })
	if err != nil {
		t.Fatal(err)
	}
	if watcher.Mode() != "polling" {
		t.Fatal("polling adapter mislabelled", watcher.Mode())
	}
	if err := os.WriteFile(filepath.Join(dir, "created"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case change := <-changes:
		if change.Error != nil || len(change.Paths) == 0 {
			t.Fatal(change)
		}
	case <-ctx.Done():
		t.Fatal("watch missed change")
	}
	if err := watcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "after-close"), []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case change := <-changes:
		t.Fatal("watch delivered after Close", change)
	default:
	}
}

func TestWatch104DirectoryBudgetCreationAndTerminalError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dir := t.TempDir()
	env := LocalEnvWithWatchOptions(dir, WatchOptions{PollInterval: 20 * time.Millisecond, MaxDirectories: 2})
	if err := os.Mkdir(filepath.Join(dir, "first"), 0700); err != nil {
		t.Fatal(err)
	}
	changes := make(chan durable.WatchChange, 8)
	watcher, err := env.Watch(ctx, []durable.WatchTarget{{Path: ".", Recursive: true}}, func(change durable.WatchChange) { changes <- change })
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close(context.Background())
	if err := os.Mkdir(filepath.Join(dir, "second"), 0700); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case change := <-changes:
			if change.Error != nil {
				if change.Error.Code != "invalid" {
					t.Fatal(change)
				}
				goto stopped
			}
		case <-ctx.Done():
			t.Fatal("watch directory limit not terminal")
		}
	}
stopped:
	if err := watcher.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := env.Watch(ctx, []durable.WatchTarget{{Path: ".", Recursive: true}}, func(durable.WatchChange) {}); err == nil {
		t.Fatal("initial watch budget not checked")
	}
}

func TestWatch104ExcludedChildrenDoNotReportParentMetadata(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dir := t.TempDir()
	env := LocalEnvWithWatchOptions(dir, WatchOptions{PollInterval: 20 * time.Millisecond})
	changes := make(chan durable.WatchChange, 16)
	watcher, err := env.Watch(ctx, []durable.WatchTarget{{Path: ".", Recursive: true, ExcludeHidden: true, ExcludeNames: []string{"ignored"}}}, func(change durable.WatchChange) { changes <- change })
	if err != nil {
		t.Fatal(err)
	}
	defer watcher.Close(context.Background())
	if err := os.WriteFile(filepath.Join(dir, ".hidden"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "ignored"), 0700); err != nil {
		t.Fatal(err)
	}
	// A visible sentinel witnesses a completed scan after excluded mutations.
	if err := os.WriteFile(filepath.Join(dir, "visible"), []byte("change"), 0600); err != nil {
		t.Fatal(err)
	}
	select {
	case change := <-changes:
		for _, path := range change.Paths {
			if filepath.Base(path) != "visible" {
				t.Fatal("excluded mutation reported", change)
			}
		}
	case <-ctx.Done():
		t.Fatal("visible sentinel missed")
	}
}

func TestWatch104TargetLinksOverlappingBudgetIdentityAndPanic(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	dir := t.TempDir()
	env := LocalEnvWithWatchOptions(dir, WatchOptions{PollInterval: 10 * time.Millisecond, MaxDirectories: 1})
	if err := os.Mkdir(filepath.Join(dir, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	// Same target twice counts each directory only once.
	w, err := env.Watch(ctx, []durable.WatchTarget{{Path: "link", Recursive: true}, {Path: "link", Recursive: true}}, func(durable.WatchChange) { panic("host callback") })
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close(context.Background())
	if err := os.WriteFile(filepath.Join(dir, "target", "child"), []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	// Synchronously check target-following and child no-follow independent of timing.
	snapshot, err := w.(*localWatcher).snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := snapshot[filepath.Join(dir, "link", "child")]; !ok {
		t.Fatal("target symlink was not traversed")
	}
	// Directory replacement with identical metadata is an identity change.
	old := snapshot[filepath.Join(dir, "link")]
	if err := os.Rename(filepath.Join(dir, "target"), filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	next, err := w.(*localWatcher).snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if sameWatchFingerprint(old, next[filepath.Join(dir, "link")]) {
		t.Fatal("directory identity ignored")
	}
	if err := os.Symlink("../old", filepath.Join(dir, "target", "descendant-link")); err != nil {
		t.Fatal(err)
	}
	next, err = w.(*localWatcher).snapshot(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next[filepath.Join(dir, "link", "descendant-link", "child")]; ok {
		t.Fatal("descendant symlink followed")
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestWatch104OverlappingTargetsKeepExplicitIdentity(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	env := LocalEnv(dir)
	if err := os.Mkdir(filepath.Join(dir, "target"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "target", "file"), []byte("data"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("target", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	for _, targets := range [][]durable.WatchTarget{
		{{Path: filepath.Join(dir, "link"), Recursive: true}, {Path: dir, Recursive: true}},
		{{Path: dir, Recursive: true}, {Path: filepath.Join(dir, "link"), Recursive: true}},
		{{Path: dir, Recursive: true}, {Path: filepath.Join(dir, "target", "file")}},
		{{Path: filepath.Join(dir, "target", "file")}, {Path: dir, Recursive: true}},
	} {
		watcher := &localWatcher{env: env, targets: targets}
		got, err := watcher.snapshot(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if info, ok := got[filepath.Join(dir, "link")]; ok && len(targets) == 2 && (targets[0].Path == filepath.Join(dir, "link") || targets[1].Path == filepath.Join(dir, "link")) && !info.Mode.IsDir() {
			t.Fatal("explicit link target overwritten by listing", targets, info.Mode)
		}
		for _, target := range targets {
			if target.Path == filepath.Join(dir, "target", "file") && got[target.Path].Size != 4 {
				t.Fatal("ancestor identity overwrote file metadata", targets, got[target.Path])
			}
		}
	}
}
