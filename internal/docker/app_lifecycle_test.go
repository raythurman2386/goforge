package docker

import (
	"errors"
	"testing"
)

func TestAppLifecycle_CreateDeleteAndPartialEnvironment(t *testing.T) {
	l := NewAppLifecycle()
	if err := l.Delete("project", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	if unlock, err := l.Create("project"); err == nil {
		unlock()
		t.Fatal("create after project deletion succeeded")
	}
	if err := l.Exclusive("environment-project", func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	unlock, err := l.Create("environment-project")
	if err != nil {
		t.Fatal("partial environment deletion blocked siblings")
	}
	unlock()
	if deleteErr := l.Delete("retry", func() error { return errors.New("transient") }); deleteErr == nil {
		t.Fatal("wanted cleanup failure")
	}
	unlock, err = l.Create("retry")
	if err != nil {
		t.Fatal("failed deletion did not permit repair")
	}
	unlock()
}
