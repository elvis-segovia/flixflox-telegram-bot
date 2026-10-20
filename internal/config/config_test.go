package config

import "testing"

func TestParseUserIDs(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    []int64
		wantErr bool
	}{
		{"un id", "265596142", []int64{265596142}, false},
		{"varios con espacios", " 1, 2 ,3 ", []int64{1, 2, 3}, false},
		{"coma colgando", "1,2,", []int64{1, 2}, false},
		{"vacío", "", nil, false},
		{"id negativo", "-1001234", []int64{-1001234}, false}, // los grupos tienen ID negativo
		{"no numérico", "1,dos,3", nil, true},
		{"con comillas", `"123"`, nil, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUserIDs(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseUserIDs(%q) no devolvió error", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseUserIDs(%q): %v", tt.raw, err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("parseUserIDs(%q) = %v, se esperaban %v", tt.raw, got, tt.want)
			}
			for _, id := range tt.want {
				if _, ok := got[id]; !ok {
					t.Errorf("falta el ID %d en %v", id, got)
				}
			}
		})
	}
}

// El bug original: parseEnvToMap recibía el valor de ALLOWED_USER_IDS y lo
// usaba como nombre de variable de entorno, así que el conjunto quedaba vacío
// y la lista vacía deja pasar a cualquiera.
func TestIsAllowedRejectsUnlistedUser(t *testing.T) {
	// Si quedara alguna lectura del entorno por el valor, este os.Setenv la
	// delataría: el ID de abajo no es el autorizado.
	t.Setenv("265596142", "999")

	cfg := &Config{AllowedUserIDs: "265596142"}
	allowed, err := parseUserIDs(cfg.AllowedUserIDs)
	if err != nil {
		t.Fatal(err)
	}
	cfg.allowedUsers = allowed

	if !cfg.IsAllowed(265596142) {
		t.Error("el usuario de la lista tendría que estar autorizado")
	}
	if cfg.IsAllowed(999) {
		t.Error("un usuario fuera de la lista no tendría que estar autorizado")
	}
	if !cfg.HasAllowList() {
		t.Error("HasAllowList = false con un ID configurado")
	}
}

func TestIsAllowedWithoutListLetsAnyoneIn(t *testing.T) {
	// Comportamiento documentado; bot.New() se niega a arrancar en este caso.
	cfg := &Config{}
	if !cfg.IsAllowed(1) {
		t.Error("sin lista se espera que pase cualquiera")
	}
	if cfg.HasAllowList() {
		t.Error("HasAllowList = true sin IDs configurados")
	}
}

func TestLoadRejectsMalformedAllowList(t *testing.T) {
	// El prefijo gana sobre el nombre pelado, así que se setea el prefijado:
	// con FLIXFLOX_ALLOWED_USER_IDS definido, envconfig ni mira ALLOWED_USER_IDS.
	t.Setenv("FLIXFLOX_BOT_TOKEN", "test-token")
	t.Setenv("FLIXFLOX_TELEGRAM_API_ENDPOINT", "http://localhost:8081/bot%s/%s")
	t.Setenv("FLIXFLOX_TELEGRAM_FILE_ENDPOINT", "http://localhost:8081/file/bot%s/%s")
	t.Setenv("FLIXFLOX_ALLOWED_USER_IDS", "123,no-soy-un-id")

	if _, err := Load(); err == nil {
		t.Error("Load() aceptó una lista de autorizados con un ID inválido")
	}

	// La misma config con la lista bien escrita tiene que cargar.
	t.Setenv("FLIXFLOX_ALLOWED_USER_IDS", "123,456")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() rechazó una config válida: %v", err)
	}
	if !cfg.IsAllowed(456) || cfg.IsAllowed(789) {
		t.Errorf("la lista no se cargó como se esperaba: %v", cfg.allowedUsers)
	}
}
