// Copyright 2017 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package dep

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/golang/dep/test"
	"github.com/sdboyer/gps"
)

func TestNewContextNoGOPATH(t *testing.T) {
	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir("src")
	h.Cd(h.Path("."))

	c, err := NewContext()
	if err == nil {
		t.Fatal("error should not have been nil")
	}

	if c != nil {
		t.Fatalf("expected context to be nil, got: %#v", c)
	}
}

func TestSplitAbsoluteProjectRoot(t *testing.T) {
	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir("src")
	h.Setenv("GOPATH", h.Path("."))
	depCtx := &Ctx{GOPATH: h.Path(".")}

	importPaths := []string{
		"github.com/pkg/errors",
		"my/silly/thing",
	}

	for _, ip := range importPaths {
		fullpath := filepath.Join(depCtx.GOPATH, "src", ip)
		pr, err := depCtx.SplitAbsoluteProjectRoot(fullpath)
		if err != nil {
			t.Fatal(err)
		}
		if pr != ip {
			t.Fatalf("expected %s, got %s", ip, pr)
		}
	}

	// test where it should return error
	pr, err := depCtx.SplitAbsoluteProjectRoot("tra/la/la/la")
	if err == nil {
		t.Fatalf("should have gotten error but did not for tra/la/la/la: %s", pr)
	}
}

func TestAbsoluteProjectRoot(t *testing.T) {
	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir("src")
	h.Setenv("GOPATH", h.Path("."))
	depCtx := &Ctx{GOPATH: h.Path(".")}

	importPaths := map[string]bool{
		"github.com/pkg/errors": true,
		"my/silly/thing":        false,
	}

	for i, create := range importPaths {
		if create {
			h.TempDir(filepath.Join("src", i))
		}
	}

	for i, ok := range importPaths {
		pr, err := depCtx.absoluteProjectRoot(i)
		if ok {
			h.Must(err)
			expected := h.Path(filepath.Join("src", i))
			if pr != expected {
				t.Fatalf("expected %s, got %q", expected, pr)
			}
			continue
		}

		if err == nil {
			t.Fatalf("expected %s to fail", i)
		}
	}

	// test that a file fails
	h.TempFile("src/thing/thing.go", "hello world")
	_, err := depCtx.absoluteProjectRoot("thing/thing.go")
	if err == nil {
		t.Fatal("error should not be nil for a file found")
	}
}

func TestVersionInWorkspace(t *testing.T) {
	test.NeedsExternalNetwork(t)
	test.NeedsGit(t)

	h := test.NewHelper(t)
	defer h.Cleanup()

	h.TempDir("src")
	h.Setenv("GOPATH", h.Path("."))
	depCtx := &Ctx{GOPATH: h.Path(".")}

	importPaths := map[string]struct {
		rev      gps.Version
		checkout bool
	}{
		"github.com/pkg/errors": {
			rev:      gps.NewVersion("v0.8.0").Is("645ef00459ed84a119197bfb8d8205042c6df63d"), // semver
			checkout: true,
		},
		"github.com/Sirupsen/logrus": {
			rev:      gps.Revision("42b84f9ec624953ecbf81a94feccb3f5935c5edf"), // random sha
			checkout: true,
		},
		"github.com/rsc/go-get-default-branch": {
			rev: gps.NewBranch("another-branch").Is("8e6902fdd0361e8fa30226b350e62973e3625ed5"),
		},
	}

	// checkout the specified revisions
	for ip, info := range importPaths {
		h.RunGo("get", ip)
		repoDir := h.Path("src/" + ip)
		if info.checkout {
			h.RunGit(repoDir, "checkout", info.rev.String())
		}

		v, err := depCtx.VersionInWorkspace(gps.ProjectRoot(ip))
		h.Must(err)

		if v != info.rev {
			t.Fatalf("expected %q, got %q", v.String(), info.rev.String())
		}
	}
}

func TestLoadProject(t *testing.T) {
	tg := test.Testgo(t)
	defer tg.Cleanup()

	tg.TempDir("src")
	tg.TempDir("src/test1")
	tg.TempDir("src/test1/sub")
	tg.TempFile("src/test1/manifest.json", `{"dependencies":{}}`)
	tg.TempFile("src/test1/lock.json", `{"memo":"cdafe8641b28cd16fe025df278b0a49b9416859345d8b6ba0ace0272b74925ee","projects":[]}`)
	tg.TempDir("src/test2")
	tg.TempDir("src/test2/sub")
	tg.TempFile("src/test2/manifest.json", `{"dependencies":{}}`)
	tg.Setenv("GOPATH", tg.Path("."))

	const ( //Path Types
		full = iota
		relative
		empty
	)

	var cases = []struct {
		lock     bool
		pathType int
		dirs     []string
	}{
		{true, full, []string{"src", "test1"}},
		{true, full, []string{"src", "test1", "sub"}},
		{false, full, []string{"src", "test2"}},
		{false, full, []string{"src", "test2", "sub"}},
		// {true, relative, []string{"src", "test1"}},
		// {true, relative, []string{"src", "test1", "sub"}},
		// {false, relative, []string{"src", "test2"}},
		// {false, relative, []string{"src", "test2", "sub"}},
		{true, empty, []string{"src", "test1"}},
		{true, empty, []string{"src", "test1", "sub"}},
		{false, empty, []string{"src", "test2"}},
		{false, empty, []string{"src", "test2", "sub"}},
	}

	for _, _case := range cases {
		ctx := &Ctx{GOPATH: tg.Path(".")}

		var proj *Project
		var err error
		var start, path string

		switch _case.pathType {
		case full:
			start = "."
			path = filepath.Join(ctx.GOPATH, filepath.Join(_case.dirs...))
		case relative:
			start = "src"
			path = filepath.Join(_case.dirs[1:]...)
		case empty:
			start = filepath.Join(_case.dirs...)
			path = ""
		}
		tg.Cd(tg.Path(start))
		proj, err = ctx.LoadProject(path)

		if err != nil {
			t.Fatalf("error in LoadProject: %q -> %s, from: %s", err.Error(), path, start)
		}
		if proj.Manifest == nil {
			t.Fatalf("Manifest file didn't load: %s, from: %s", path, start)
		}
		if _case.lock && proj.Lock == nil {
			t.Fatalf("Lock file didn't load -> %s, from: %s", path, start)
		} else if !_case.lock && proj.Lock != nil {
			t.Fatalf("Non-existent Lock file loaded -> %s, from: %s", path, start)
		}
	}
}

func TestLoadProjectNotFoundErrors(t *testing.T) {
	tg := test.Testgo(t)
	defer tg.Cleanup()

	tg.TempDir("src")
	tg.TempDir("src/test1")
	tg.TempDir("src/test1/sub")
	tg.Setenv("GOPATH", tg.Path("."))

	var cases = []struct {
		fromPath bool
		dirs     []string
	}{
		{true, []string{"src", "test1"}},
		{true, []string{"src", "test1", "sub"}},
		{false, []string{"src", "test1"}},
		{false, []string{"src", "test1", "sub"}},
	}

	for _, _case := range cases {
		ctx := &Ctx{GOPATH: tg.Path(".")}
		local := filepath.Join(_case.dirs...)
		path := filepath.Join(ctx.GOPATH, local)

		var err error
		if _case.fromPath {
			tg.Cd(tg.Path("."))
			_, err = ctx.LoadProject(path)
		} else {
			tg.Cd(tg.Path(local))
			_, err = ctx.LoadProject("")
		}
		if err == nil {
			t.Fatalf("should have thrown 'No Manifest Found' error -> %s", local)
		}
	}
}

func TestLoadProjectManifestParseError(t *testing.T) {
	tg := test.Testgo(t)
	defer tg.Cleanup()

	tg.TempDir("src")
	tg.TempDir("src/test1")
	tg.TempFile("src/test1/manifest.json", ` "dependencies":{} `)
	tg.TempFile("src/test1/lock.json", `{"memo":"cdafe8641b28cd16fe025df278b0a49b9416859345d8b6ba0ace0272b74925ee","projects":[]}`)
	tg.Setenv("GOPATH", tg.Path("."))

	ctx := &Ctx{GOPATH: tg.Path(".")}
	path := filepath.Join("src", "test1")
	tg.Cd(tg.Path(path))

	_, err := ctx.LoadProject("")
	if err == nil {
		t.Fatalf("should have thrown 'Manifest Syntax' error")
	}
}

func TestLoadProjectLockParseError(t *testing.T) {
	tg := test.Testgo(t)
	defer tg.Cleanup()

	tg.TempDir("src")
	tg.TempDir("src/test1")
	tg.TempFile("src/test1/manifest.json", `{"dependencies":{}}`)
	tg.TempFile("src/test1/lock.json", ` "memo":"cdafe8641b28cd16fe025df278b0a49b9416859345d8b6ba0ace0272b74925ee","projects":[] `)
	tg.Setenv("GOPATH", tg.Path("."))

	ctx := &Ctx{GOPATH: tg.Path(".")}
	path := filepath.Join("src", "test1")
	tg.Cd(tg.Path(path))

	_, err := ctx.LoadProject("")
	if err == nil {
		t.Fatalf("should have thrown 'Lock Syntax' error")
	}
}

func TestLoadProjectNoSrcDir(t *testing.T) {
	tg := test.Testgo(t)
	defer tg.Cleanup()

	tg.TempDir("test1")
	tg.TempFile("test1/manifest.json", `{"dependencies":{}}`)
	tg.TempFile("test1/lock.json", `{"memo":"cdafe8641b28cd16fe025df278b0a49b9416859345d8b6ba0ace0272b74925ee","projects":[]}`)
	tg.Setenv("GOPATH", tg.Path("."))

	ctx := &Ctx{GOPATH: tg.Path(".")}
	path := filepath.Join("test1")
	tg.Cd(tg.Path(path))

	f, _ := os.OpenFile(filepath.Join(ctx.GOPATH, "src", "test1", "lock.json"), os.O_WRONLY, os.ModePerm)
	defer f.Close()

	_, err := ctx.LoadProject("")
	if err == nil {
		t.Fatalf("should have thrown 'Split Absolute Root' error (no 'src' dir present)")
	}
}
