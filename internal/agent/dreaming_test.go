package agent

import (
	"log/slog"
	"strings"
	"testing"
)

func TestDreamingService_EnsureJobs(t *testing.T) {
	store, _ := NewJobStore(t.TempDir() + "/jobs.json")
	svc := NewDreamingService(store, nil, slog.Default())

	if err := svc.EnsureJobs(); err != nil {
		t.Fatal(err)
	}

	// Should have 3 jobs
	jobs := store.List()
	if len(jobs) != 3 {
		t.Fatalf("got %d jobs, want 3", len(jobs))
	}

	// Verify job IDs
	ids := map[string]bool{}
	for _, j := range jobs {
		ids[j.ID] = true
	}
	for _, expected := range []string{"dreaming-light", "dreaming-deep", "dreaming-rem"} {
		if !ids[expected] {
			t.Errorf("missing job %q", expected)
		}
	}

	// All should be disabled by default
	for _, j := range jobs {
		if j.Enabled {
			t.Errorf("job %s should be disabled by default", j.ID)
		}
	}

	// Idempotent: calling again should not create duplicates
	svc.EnsureJobs()
	if len(store.List()) != 3 {
		t.Errorf("idempotent check failed: got %d jobs, want 3", len(store.List()))
	}
}

func TestDreamingService_EnableDisable(t *testing.T) {
	store, _ := NewJobStore(t.TempDir() + "/jobs.json")
	svc := NewDreamingService(store, nil, slog.Default())
	svc.EnsureJobs()

	// Enable all
	if err := svc.Enable(); err != nil {
		t.Fatal(err)
	}
	for _, j := range store.List() {
		if !j.Enabled {
			t.Errorf("job %s should be enabled", j.ID)
		}
	}

	status := svc.Status()
	if !status.Enabled {
		t.Error("status should report enabled")
	}

	// Disable all
	if err := svc.Disable(); err != nil {
		t.Fatal(err)
	}
	for _, j := range store.List() {
		if j.Enabled {
			t.Errorf("job %s should be disabled", j.ID)
		}
	}

	status = svc.Status()
	if status.Enabled {
		t.Error("status should report disabled")
	}
}

func TestDreamingService_Status(t *testing.T) {
	store, _ := NewJobStore(t.TempDir() + "/jobs.json")
	svc := NewDreamingService(store, nil, slog.Default())
	svc.EnsureJobs()

	status := svc.Status()
	if len(status.Phases) != 3 {
		t.Fatalf("got %d phases, want 3", len(status.Phases))
	}

	phases := map[DreamPhase]bool{}
	for _, p := range status.Phases {
		phases[p.Phase] = true
	}
	for _, expected := range []DreamPhase{PhaseLight, PhaseDeep, PhaseREM} {
		if !phases[expected] {
			t.Errorf("missing phase %q", expected)
		}
	}
}

func TestDreamingService_Schedules(t *testing.T) {
	store, _ := NewJobStore(t.TempDir() + "/jobs.json")
	svc := NewDreamingService(store, nil, slog.Default())
	svc.EnsureJobs()

	light, _ := store.Get("dreaming-light")
	if light.Schedule.Kind != ScheduleEvery || light.Schedule.Every != "6h" {
		t.Errorf("light schedule = %+v, want every:6h", light.Schedule)
	}

	deep, _ := store.Get("dreaming-deep")
	if deep.Schedule.Kind != ScheduleCron || deep.Schedule.Expr != "0 3 * * *" {
		t.Errorf("deep schedule = %+v, want cron:0 3 * * *", deep.Schedule)
	}

	rem, _ := store.Get("dreaming-rem")
	if rem.Schedule.Kind != ScheduleCron || rem.Schedule.Expr != "0 5 * * 0" {
		t.Errorf("rem schedule = %+v, want cron:0 5 * * 0", rem.Schedule)
	}
}

func TestDreamingPrompts_NonEmpty(t *testing.T) {
	prompts := []struct {
		name   string
		prompt string
		keys   []string
	}{
		{"light", lightDreamingPrompt(), []string{"LIGHT", "memory_read", "memory_write", "daily"}},
		{"deep", deepDreamingPrompt(), []string{"DEEP", "memory_read", "MEMORY.md", "curated"}},
		{"rem", remDreamingPrompt(), []string{"REM", "Dream Diary", "memory_read", "memory_write"}},
	}

	for _, tc := range prompts {
		if tc.prompt == "" {
			t.Errorf("%s prompt should not be empty", tc.name)
		}
		for _, key := range tc.keys {
			if !strings.Contains(tc.prompt, key) {
				t.Errorf("%s prompt should contain %q", tc.name, key)
			}
		}
	}
}
