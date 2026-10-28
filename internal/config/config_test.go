package config

import "testing"

func TestParseUserIDs(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []int64
		wantErr bool
	}{
		{"one id", "265596142", []int64{265596142}, false},
		{"several with spaces", " 1, 2 ,3 ", []int64{1, 2, 3}, false},
		{"trailing comma", "1,2,", []int64{1, 2}, false},
		{"empty", "", nil, false},
		{"negative id", "-1001234", []int64{-1001234}, false}, // groups have a negative ID
		{"not a number", "1,two,3", nil, true},
		{"quoted", `"123"`, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUserIDs(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseUserIDs(%q) returned no error", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseUserIDs(%q): %v", tt.raw, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseUserIDs(%q) = %v, want %v", tt.raw, got, tt.want)
			}
			for _, id := range tt.want {
				if _, ok := got[id]; !ok {
					t.Errorf("ID %d missing from %v", id, got)
				}
			}
		})
	}
}

// The original bug: parseEnvToMap took the value of ALLOWED_USER_IDS and used
// it as an environment variable name, so the set came out empty — and an empty
// list lets anyone through.
func TestIsAllowedRejectsUnlistedUser(t *testing.T) {
	// If any lookup by value were left, this os.Setenv would expose it: the ID
	// below is not the authorized one.
	t.Setenv("265596142", "999")

	cfg := &Config{AllowedUserIDs: "265596142"}
	allowed, err := parseUserIDs(cfg.AllowedUserIDs)
	if err != nil {
		t.Fatal(err)
	}
	cfg.allowedUsers = allowed

	if !cfg.IsAllowed(265596142) {
		t.Error("the user on the list should be authorized")
	}
	if cfg.IsAllowed(999) {
		t.Error("a user outside the list should not be authorized")
	}
	if !cfg.HasAllowList() {
		t.Error("HasAllowList = false with one ID configured")
	}
}

func TestIsAllowedWithoutListLetsAnyoneIn(t *testing.T) {
	// Documented behavior; bot.New() refuses to start in this case.
	cfg := &Config{}
	if !cfg.IsAllowed(1) {
		t.Error("without a list anyone is expected to get through")
	}
	if cfg.HasAllowList() {
		t.Error("HasAllowList = true with no IDs configured")
	}
}

func TestLoadRejectsMalformedAllowList(t *testing.T) {
	// The prefixed name wins over the bare one, so the prefixed one is set:
	// with FLIXFLOX_ALLOWED_USER_IDS defined, envconfig never looks at
	// ALLOWED_USER_IDS.
	t.Setenv("FLIXFLOX_BOT_TOKEN", "test-token")
	t.Setenv("FLIXFLOX_TELEGRAM_API_ENDPOINT", "http://localhost:8081/bot%s/%s")
	t.Setenv("FLIXFLOX_TELEGRAM_FILE_ENDPOINT", "http://localhost:8081/file/bot%s/%s")
	t.Setenv("FLIXFLOX_ALLOWED_USER_IDS", "123,not-an-id")

	if _, err := Load(); err == nil {
		t.Error("Load() accepted an allow list with an invalid ID")
	}

	// The same config with the list written properly has to load.
	t.Setenv("FLIXFLOX_ALLOWED_USER_IDS", "123,456")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() rejected a valid config: %v", err)
	}
	if !cfg.IsAllowed(456) || cfg.IsAllowed(789) {
		t.Errorf("the list was not loaded as expected: %v", cfg.allowedUsers)
	}
}
