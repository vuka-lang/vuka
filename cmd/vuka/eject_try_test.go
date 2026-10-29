package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestEjectTry ejects code using ? in expressions and headers, ?(…) and ?
// in main and tests: go build, go vet and go test take the Go as written
// by hand, and it does what the .vuka did.
func TestEjectTry(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	root := t.TempDir()
	gomod, gosum := testModule(t, "example.com/app")
	writeFile(t, filepath.Join(root, "go.mod"), gomod)
	writeFile(t, filepath.Join(root, "go.sum"), gosum)
	writeFile(t, filepath.Join(root, "users/users.vuka"), `package users

import (
	"errors"
	"strings"
)

var ErrNotFound = errors.New("not found")

enum RegisterError {
	EmailTaken
	Database{Err error}
}

func (RegisterError.EmailTaken) Error() string { return "email already registered" }
func (e RegisterError.Database) Error() string { return "database: " + e.Err.Error() }

type User struct {
	Email  string
	Active bool
}

type Store struct{ users map[string]*User }

func NewStore() *Store { return &Store{users: map[string]*User{}} }

func (s *Store) Exists(email string) (bool, error) {
	if email == "" {
		return false, ErrNotFound
	}
	_, ok := s.users[email]
	return ok, nil
}

func (s *Store) Create(email string) (*User, error) {
	if strings.HasPrefix(email, "down") {
		return nil, errors.New("connection refused")
	}
	u := &User{Email: email, Active: true}
	s.users[email] = u
	return u, nil
}

func (s *Store) CanPost(u *User) (bool, error) { return u.Active, nil }

// Register signs a user up.
func (s *Store) Register(email string) (*User, error) {
	if s.Exists(email)?("checking %s", email) {
		return nil, RegisterError.EmailTaken
	}
	return s.Create(email)?(RegisterError.Database), nil
}

// Allowed reports whether email may post.
func (s *Store) Allowed(email string) (bool, error) {
	u := s.users[email]
	if u != nil && u.Active && s.CanPost(u)? {
		return true, nil
	}
	return false, nil
}

// Count counts the users while next says there are more.
func (s *Store) Count(next func(int) (bool, error)) (int, error) {
	n := 0
	for next(n)? {
		n++
	}
	return n, nil
}
`)
	writeFile(t, filepath.Join(root, "users/users_test.vuka"), `package users

import "testing"

func TestRegister(t *testing.T) {
	s := NewStore()
	u := s.Register("ann@example.com")?
	if !s.Allowed(u.Email)? {
		t.Fatal("ann can't post")
	}
}
`)
	writeFile(t, filepath.Join(root, "cmd/app/main.vuka"), `package main

import (
	"fmt"
	"os"

	"example.com/app/users"
)

func main() {
	defer fmt.Println("done")
	s := users.NewStore()
	fmt.Println(s.Count(func(n int) (bool, error) { return n < 3, nil })?)
	s.Register(os.Args[1])?
	s.Register(os.Args[1])?
	fmt.Println("registered twice")
}
`)
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod")
	t.Chdir(root)
	var out bytes.Buffer
	if err := eject([]string{"-rm", "./..."}, &out); err != nil {
		t.Fatalf("%v\n%s", err, out.String())
	}
	bin := t.TempDir()
	for _, args := range [][]string{{"build", "-o", bin, "./..."}, {"vet", "./..."}, {"test", "./..."}} {
		if out, err := exec.Command("go", args...).CombinedOutput(); err != nil {
			t.Fatalf("go %s after vuka eject -rm: %v\n%s", args[0], err, out)
		}
	}
	src, _ := os.ReadFile("users/users.go")
	for _, want := range []string{
		"exists, err := s.Exists(email)",
		`return nil, fmt.Errorf("checking %s: %w", email, err)`,
		"return nil, RegisterErrorDatabase{Err: err}",
		"if u != nil && u.Active {\n\t\tcanPost, err := s.CanPost(u)",
		"for {\n\t\tnext, err := next(n)",
	} {
		if !bytes.Contains(src, []byte(want)) {
			t.Errorf("users.go has no %q:\n%s", want, src)
		}
	}
	cmd := exec.Command(filepath.Join(bin, "app"), "ann@example.com")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("app: %v\n%s%s", err, stdout.String(), stderr.String())
	}
	if stdout.String() != "3\ndone\n" || strings.TrimSpace(stderr.String()) != "error: email already registered" {
		t.Errorf("app printed %q and %q", stdout.String(), stderr.String())
	}
}
