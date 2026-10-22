package bot

import (
	"reflect"
	"testing"
)

// sampleAnswers tiene una respuesta válida por paso. Un paso nuevo sin entrada
// acá rompe los tests a propósito: así no se agrega al flujo sin cubrirlo.
var sampleAnswers = map[stage]string{
	stageAwaitingTitle:                   "Blade Runner",
	stageAwaitingDescription:             "Un cazador de replicantes",
	stageAwaitingYear:                    "1982",
	stageAwaitingGenre:                   "sci-fi, noir",
	stageAwaitingRating:                  "8.1",
	stageAwaitingBackground:              "https://example.test/bg.jpg",
	stageAwaitingSeasonNumber:            "1",
	stageAwaitingEpisodeNumber:           "2",
	stageAwaitingEpisodeTitle:            "Segundo episodio",
	stageAwaitingSkipIntroDisplayMessage: "Skip intro",
	stageAwaitingSkipIntroStartTime:      "0:30",
	stageAwaitingSkipIntroEndTime:        "1:30",
	stageAwaitingNextEpisode:             "42:10",
}

func TestFlowsEndOnTheFileStep(t *testing.T) {
	for name, flow := range map[string][]step{"movie": movieFlow, "tv-show": tvShowFlow} {
		if len(flow) == 0 {
			t.Fatalf("el flujo %s está vacío", name)
		}

		last := flow[len(flow)-1]
		if last.stage != stageAwaitingFile {
			t.Errorf("el flujo %s termina en la etapa %d, se esperaba el archivo", name, last.stage)
		}
		if !last.media {
			t.Errorf("el último paso del flujo %s no acepta adjuntos", name)
		}

		seen := map[stage]bool{}
		for _, st := range flow {
			if seen[st.stage] {
				t.Errorf("el flujo %s repite la etapa %d", name, st.stage)
			}
			seen[st.stage] = true
		}
	}
}

// Una película no tiene temporada ni episodio: antes se los preguntaba igual.
func TestMovieFlowSkipsEpisodeSteps(t *testing.T) {
	for _, st := range movieFlow {
		for _, episodeStep := range episodeSteps {
			if st.stage == episodeStep.stage {
				t.Errorf("el flujo de películas incluye la etapa de serie %d", st.stage)
			}
		}
	}
}

// Invariante central: cada paso escribe en su propio campo. El bug original
// era que de la temporada en adelante todos guardaban en SeasonNumber.
func TestEachStepWritesItsOwnField(t *testing.T) {
	writers := map[string]stage{}

	for _, st := range tvShowFlow {
		if st.assign == nil {
			continue
		}

		answer, ok := sampleAnswers[st.stage]
		if !ok {
			t.Fatalf("falta una respuesta de ejemplo para la etapa %d", st.stage)
		}

		sess := session{Category: categoryTVShow, Stage: st.stage}
		if err := st.assign(&sess, answer); err != nil {
			t.Fatalf("la etapa %d rechazó %q: %v", st.stage, answer, err)
		}

		changed := changedFields(session{Category: categoryTVShow, Stage: st.stage}, sess)
		if len(changed) != 1 {
			t.Fatalf("la etapa %d escribió %v, se esperaba un solo campo", st.stage, changed)
		}
		if other, dup := writers[changed[0]]; dup {
			t.Errorf("las etapas %d y %d escriben las dos en %s", other, st.stage, changed[0])
		}
		writers[changed[0]] = st.stage
	}
}

// Caminar el flujo entero verifica que cada paso lleve al siguiente y que la
// serie llegue al archivo pasando por todas las etapas.
func TestWalkingTheFlowReachesTheFileStep(t *testing.T) {
	sess := session{Category: categoryTVShow, Stage: stageAwaitingTitle}

	for range len(tvShowFlow) {
		st, ok := stepFor(sess)
		if !ok {
			t.Fatalf("la etapa %d no está en el flujo", sess.Stage)
		}
		if st.assign == nil { // el paso del archivo
			break
		}

		if err := st.assign(&sess, sampleAnswers[st.stage]); err != nil {
			t.Fatalf("la etapa %d rechazó la respuesta: %v", st.stage, err)
		}
		next, ok := nextStep(sess)
		if !ok {
			t.Fatalf("la etapa %d no tiene siguiente", sess.Stage)
		}
		sess.Stage = next.stage
	}

	if sess.Stage != stageAwaitingFile {
		t.Errorf("el flujo terminó en la etapa %d, se esperaba el archivo", sess.Stage)
	}
	if sess.SeasonNumber != "1" || sess.EpisodeNumber != "2" || sess.EpisodeTitle != "Segundo episodio" {
		t.Errorf("se perdieron datos del episodio: %+v", sess)
	}
}

// El título es el nombre de la carpeta: no se puede saltear ni aceptar sin sanear.
func TestTitleStepIsRequiredAndSanitized(t *testing.T) {
	st := tvShowFlow[0]
	if st.stage != stageAwaitingTitle {
		t.Fatalf("el primer paso es la etapa %d, se esperaba el título", st.stage)
	}
	if st.skippable {
		t.Error("el título no debería poder saltearse")
	}

	var sess session
	if err := st.assign(&sess, "../../etc"); err != nil {
		t.Fatalf("título rechazado de más: %v", err)
	}
	if sess.Title != "_.._etc" {
		t.Errorf("Title = %q, se esperaba el nombre saneado", sess.Title)
	}

	if err := st.assign(&sess, ".."); err == nil {
		t.Error("un título que no deja nada usable tendría que rechazarse")
	}
}

// El usuario elige números, pero lo que se guarda son los nombres: la
// numeración existe solo en la conversación.
func TestGenreStepResolvesNumbersAgainstTheListShown(t *testing.T) {
	list := []string{"Action", "Sci-Fi", "Noir"}

	tests := []struct {
		name   string
		answer string
		want   string
	}{
		{"un número", "2", "Sci-Fi"},
		{"separados por espacio", "1 3", "Action, Noir"},
		{"separados por coma", "1, 3", "Action, Noir"},
		{"repetido", "2 2", "Sci-Fi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := session{Genres: list}
			if err := assignGenres(&sess, tt.answer); err != nil {
				t.Fatalf("%q rechazado: %v", tt.answer, err)
			}
			if sess.Genre != tt.want {
				t.Errorf("Genre = %q, se esperaba %q", sess.Genre, tt.want)
			}
		})
	}

	// Un número fuera de la lista se rechaza entero: guardar los válidos y
	// descartar el resto en silencio carga un género que nadie eligió.
	for _, answer := range []string{"0", "4", "sci-fi", "1 4", ""} {
		sess := session{Genres: list}
		if err := assignGenres(&sess, answer); err == nil {
			t.Errorf("%q tendría que rechazarse, quedó Genre = %q", answer, sess.Genre)
		}
	}
}

// Si el API no contestó la lista queda vacía y el paso acepta texto libre, en
// vez de dejar la carga trabada en un género.
func TestGenreStepFallsBackToFreeTextWithoutList(t *testing.T) {
	var sess session
	if err := assignGenres(&sess, "sci-fi, noir"); err != nil {
		t.Fatalf("texto libre rechazado: %v", err)
	}
	if sess.Genre != "sci-fi, noir" {
		t.Errorf("Genre = %q, se esperaba el texto tal cual", sess.Genre)
	}
}

func TestVideoBaseName(t *testing.T) {
	tests := []struct {
		name string
		sess session
		want string
	}{
		{
			"película con año",
			session{Category: categoryMovie, Title: "Blade Runner", ReleaseYear: "1982"},
			"Blade Runner (1982)",
		},
		{
			"película sin año",
			session{Category: categoryMovie, Title: "Blade Runner"},
			"Blade Runner",
		},
		{
			"episodio con título",
			session{Category: categoryTVShow, SeasonNumber: "1", EpisodeNumber: "2", EpisodeTitle: "Piloto"},
			"S01E02 - Piloto",
		},
		{
			"episodio sin título",
			session{Category: categoryTVShow, SeasonNumber: "10", EpisodeNumber: "11"},
			"S10E11",
		},
		{
			// Sin números no se inventa nombre: queda el que trae el archivo.
			"episodio sin números",
			session{Category: categoryTVShow},
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := videoBaseName(tt.sess); got != tt.want {
				t.Errorf("videoBaseName() = %q, se esperaba %q", got, tt.want)
			}
		})
	}
}

func TestValidTimecode(t *testing.T) {
	valid := []string{"0", "90", "123456", "1:30", "0:05", "1:02:03"}
	for _, in := range valid {
		if !validTimecode(in) {
			t.Errorf("%q tendría que aceptarse", in)
		}
	}

	invalid := []string{"", ":", "1:", ":30", "1:2", "1:2:3", "90s", "-5", "1:30:00:00", "uno:dos"}
	for _, in := range invalid {
		if validTimecode(in) {
			t.Errorf("%q tendría que rechazarse", in)
		}
	}
}

// changedFields lista los campos de texto que difieren entre dos sesiones.
func changedFields(before, after session) []string {
	var changed []string

	beforeValue := reflect.ValueOf(before)
	afterValue := reflect.ValueOf(after)
	for i := range beforeValue.NumField() {
		if beforeValue.Field(i).Kind() != reflect.String {
			continue
		}
		if beforeValue.Field(i).String() != afterValue.Field(i).String() {
			changed = append(changed, beforeValue.Type().Field(i).Name)
		}
	}
	return changed
}
