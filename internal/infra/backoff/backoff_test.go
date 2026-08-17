package backoff

import (
	"testing"
	"time"
)

func TestLinear(t *testing.T) {
	cases := []struct {
		name    string
		attempt int
		step    time.Duration
		want    time.Duration
	}{
		{"zero attempt", 0, time.Second, 0},
		{"negative attempt", -1, time.Second, 0},
		{"first attempt", 1, 100 * time.Millisecond, 100 * time.Millisecond},
		{"third attempt", 3, 100 * time.Millisecond, 300 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Linear(tc.attempt, tc.step); got != tc.want {
				t.Errorf("Linear(%d, %v) = %v, want %v", tc.attempt, tc.step, got, tc.want)
			}
		})
	}
}

func TestExponential_Next(t *testing.T) {
	p := NewExponential(100*time.Millisecond, 800*time.Millisecond)

	want := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		800 * time.Millisecond,
		800 * time.Millisecond,
	}
	for i, w := range want {
		if got := p.Next(); got != w {
			t.Errorf("Next() #%d = %v, want %v", i+1, got, w)
		}
	}
}

func TestExponential_Reset(t *testing.T) {
	p := NewExponential(100*time.Millisecond, time.Second)
	p.Next()
	p.Next()
	p.Reset()

	if got := p.Next(); got != 100*time.Millisecond {
		t.Errorf("Next() after Reset() = %v, want 100ms", got)
	}
}

func TestExponential_Defaults(t *testing.T) {
	p := NewExponential(0, 0)
	if got := p.Next(); got != 100*time.Millisecond {
		t.Errorf("Next() with zero config = %v, want default 100ms", got)
	}
}

func TestWait_NilStop(t *testing.T) {
	start := time.Now()
	if stopped := Wait(nil, 10*time.Millisecond); stopped {
		t.Error("Wait(nil, ...) returned stopped=true")
	}
	if elapsed := time.Since(start); elapsed < 5*time.Millisecond {
		t.Errorf("Wait(nil, 10ms) returned after %v", elapsed)
	}
}

func TestWait_StopFires(t *testing.T) {
	stop := make(chan struct{})
	go func() {
		time.Sleep(5 * time.Millisecond)
		close(stop)
	}()

	if !Wait(stop, time.Second) {
		t.Error("Wait(...) returned stopped=false when stop fired")
	}
}

func TestWait_ZeroDelay(t *testing.T) {
	if stopped := Wait(make(chan struct{}), 0); stopped {
		t.Error("Wait(_, 0) returned stopped=true")
	}
}
