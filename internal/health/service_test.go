package health

import (
	"context"
	"errors"
	"testing"
)

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestLiveAlwaysOK(t *testing.T) {
	svc := NewService("test", nil)
	resp := svc.Live()
	if resp.Status != StatusOK {
		t.Fatalf("want ok, got %s", resp.Status)
	}
	if resp.Version != "test" {
		t.Fatalf("want test, got %s", resp.Version)
	}
}

func TestReadyOKWhenDbUp(t *testing.T) {
	svc := NewService("test", fakePinger{})
	resp, err := svc.Ready(context.Background())
	if err != nil {
		t.Fatalf("want nil err, got %v", err)
	}
	if resp.Status != StatusOK || resp.Checks["db"] != StatusOK {
		t.Fatalf("want ok checks, got %+v", resp)
	}
}

func TestReadyFailWhenDbDown(t *testing.T) {
	svc := NewService("test", fakePinger{err: errors.New("down")})
	resp, err := svc.Ready(context.Background())
	if err == nil {
		t.Fatal("want err, got nil")
	}
	if resp.Status != StatusFail || resp.Checks["db"] != "fail" {
		t.Fatalf("want fail checks, got %+v", resp)
	}
}
