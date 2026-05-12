package coding

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadTemplates_DiscoverAndOverride(t *testing.T) {
	home := t.TempDir()
	repo := filepath.Join(home, "repo")
	mustMkdir(t, filepath.Join(repo, ".git"))
	mustWrite(t, filepath.Join(home, ".claude", "commands", "review.md"),
		"---\ndescription: global review\n---\nGlobal body $1")
	mustWrite(t, filepath.Join(repo, ".claude", "commands", "review.md"),
		"---\ndescription: project review\nargument-hint: <pr>\n---\nProject body $@")

	tmpls, err := loadTemplatesWithHome(t.Context(), repo, home)
	if err != nil {
		t.Fatalf("LoadTemplates: %v", err)
	}
	if len(tmpls) != 1 {
		t.Fatalf("want 1 (project overrides global), got %d", len(tmpls))
	}
	if tmpls[0].Description != "project review" {
		t.Errorf("description=%q", tmpls[0].Description)
	}
	if tmpls[0].ArgumentHint != "<pr>" {
		t.Errorf("argument-hint=%q", tmpls[0].ArgumentHint)
	}
}

func TestTemplateExpand_Interpolation(t *testing.T) {
	tt := &Template{Body: "P=$1 Q=$2 ALL=$@ BAR=$ARGUMENTS"}
	got, err := tt.Expand([]string{"a", "b", "c"})
	if err != nil {
		t.Fatal(err)
	}
	want := "P=a Q=b ALL=a b c BAR=a b c"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestTemplateExpand_Slicing(t *testing.T) {
	tt := &Template{Body: "TAIL=${@:2} MID=${@:2:2}"}
	got, err := tt.Expand([]string{"a", "b", "c", "d"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "TAIL=b c d") {
		t.Errorf("got=%q", got)
	}
	if !strings.Contains(got, "MID=b c") {
		t.Errorf("got=%q", got)
	}
}

func TestTemplateExpand_NonRecursive(t *testing.T) {
	tt := &Template{Body: "X=$1"}
	got, err := tt.Expand([]string{"$2 lol"})
	if err != nil {
		t.Fatal(err)
	}
	if got != "X=$2 lol" {
		t.Fatalf("expected non-recursive: %q", got)
	}
}

func TestSlashRegistry_Resolve_Template(t *testing.T) {
	reg := &SlashRegistry{
		templates: map[string]*Template{
			"foo": {Name: "foo", Body: "Hello $1"},
		},
		builtins: map[string]Builtin{},
	}
	res, err := reg.Resolve("/foo world")
	if err != nil {
		t.Fatal(err)
	}
	if !res.IsSlash || res.IsBuiltin || res.Literal != "Hello world" {
		t.Fatalf("res=%+v", res)
	}
}

func TestSlashRegistry_Resolve_Unknown(t *testing.T) {
	reg := &SlashRegistry{templates: map[string]*Template{}, builtins: map[string]Builtin{}}
	_, err := reg.Resolve("/nope")
	if err == nil || !strings.Contains(err.Error(), "unknown") {
		t.Fatalf("expected unknown error, got %v", err)
	}
}

func TestSlashRegistry_Resolve_NonSlashPassThrough(t *testing.T) {
	reg := &SlashRegistry{templates: map[string]*Template{}, builtins: map[string]Builtin{}}
	res, err := reg.Resolve("not a slash")
	if err != nil {
		t.Fatal(err)
	}
	if res.IsSlash {
		t.Fatalf("should not be slash")
	}
	if res.Literal != "not a slash" {
		t.Fatalf("literal=%q", res.Literal)
	}
}

func TestParseSlashLine(t *testing.T) {
	cases := []struct {
		in     string
		name   string
		args   []string
		isSlsh bool
	}{
		{"hello", "", nil, false},
		{"/foo", "foo", []string{}, true},
		{"/foo bar baz", "foo", []string{"bar", "baz"}, true},
		{`/foo "hello world" zzz`, "foo", []string{"hello world", "zzz"}, true},
	}
	for _, c := range cases {
		name, args, isS := parseSlashLine(c.in)
		if isS != c.isSlsh || name != c.name || !reflect.DeepEqual(args, c.args) {
			t.Errorf("parseSlashLine(%q)=(%q,%v,%v) want (%q,%v,%v)",
				c.in, name, args, isS, c.name, c.args, c.isSlsh)
		}
	}
}
