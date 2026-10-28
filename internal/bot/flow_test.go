package bot

import (
	"reflect"
	"testing"
)

// sampleAnswers holds one valid answer per step. A new step with no entry here
// breaks the tests on purpose: that way it cannot be added to the flow without
// being covered.
var sampleAnswers = map[stage]string{
	stageAwaitingTitle:                   "Blade Runner",
	stageAwaitingDescription:             "A blade runner hunting replicants",
	stageAwaitingYear:                    "1982",
	stageAwaitingGenre:                   "sci-fi, noir",
	stageAwaitingRating:                  "8.1",
	stageAwaitingBackground:              "https://example.test/bg.jpg",
	stageAwaitingSeasonNumber:            "1",
	stageAwaitingEpisodeNumber:           "2",
	stageAwaitingEpisodeTitle:            "Second episode",
	stageAwaitingSkipIntroDisplayMessage: "Skip intro",
	stageAwaitingSkipIntroStartTime:      "0:30",
	stageAwaitingSkipIntroEndTime:        "1:30",
	stageAwaitingNextEpisode:             "42:10",
}

func TestFlowsEndOnTheFileStep(t *testing.T) {
	for name, flow := range map[string][]step{"movie": movieFlow, "tv-show": tvShowFlow} {
		if len(flow) == 0 {
			t.Fatalf("the %s flow is empty", name)
		}

		last := flow[len(flow)-1]
		if last.stage != stageAwaitingFile {
			t.Errorf("the %s flow ends on stage %d, want the file", name, last.stage)
		}
		if !last.media {
			t.Errorf("the last step of the %s flow does not accept attachments", name)
		}

		seen := map[stage]bool{}
		for _, st := range flow {
			if seen[st.stage] {
				t.Errorf("the %s flow repeats stage %d", name, st.stage)
			}
			seen[st.stage] = true
		}
	}
}

// A movie has no season or episode: it used to be asked for them anyway.
func TestMovieFlowSkipsEpisodeSteps(t *testing.T) {
	for _, st := range movieFlow {
		for _, episodeStep := range episodeSteps {
			if st.stage == episodeStep.stage {
				t.Errorf("the movie flow includes the TV show stage %d", st.stage)
			}
		}
	}
}

// Core invariant: each step writes to its own field. The original bug was that
// from the season onwards they all stored into SeasonNumber.
func TestEachStepWritesItsOwnField(t *testing.T) {
	writers := map[string]stage{}

	for _, st := range tvShowFlow {
		if st.assign == nil {
			continue
		}

		answer, ok := sampleAnswers[st.stage]
		if !ok {
			t.Fatalf("a sample answer for stage %d is missing", st.stage)
		}

		sess := session{Category: categoryTVShow, Stage: st.stage}
		if err := st.assign(&sess, answer); err != nil {
			t.Fatalf("stage %d rejected %q: %v", st.stage, answer, err)
		}

		changed := changedFields(session{Category: categoryTVShow, Stage: st.stage}, sess)
		if len(changed) != 1 {
			t.Fatalf("stage %d wrote %v, want a single field", st.stage, changed)
		}
		if other, dup := writers[changed[0]]; dup {
			t.Errorf("stages %d and %d both write to %s", other, st.stage, changed[0])
		}
		writers[changed[0]] = st.stage
	}
}

// Walking the whole flow checks that each step leads to the next one and that
// a TV show reaches the file after going through every stage.
func TestWalkingTheFlowReachesTheFileStep(t *testing.T) {
	sess := session{Category: categoryTVShow, Stage: stageAwaitingTitle}

	for range len(tvShowFlow) {
		st, ok := stepFor(sess)
		if !ok {
			t.Fatalf("stage %d is not in the flow", sess.Stage)
		}
		if st.assign == nil { // the file step
			break
		}

		if err := st.assign(&sess, sampleAnswers[st.stage]); err != nil {
			t.Fatalf("stage %d rejected the answer: %v", st.stage, err)
		}
		next, ok := nextStep(sess)
		if !ok {
			t.Fatalf("stage %d has no next one", sess.Stage)
		}
		sess.Stage = next.stage
	}

	if sess.Stage != stageAwaitingFile {
		t.Errorf("the flow ended on stage %d, want the file", sess.Stage)
	}
	if sess.SeasonNumber != "1" || sess.EpisodeNumber != "2" || sess.EpisodeTitle != "Second episode" {
		t.Errorf("episode data was lost: %+v", sess)
	}
}

// The title is the folder name: it can neither be skipped nor accepted without
// sanitizing.
func TestTitleStepIsRequiredAndSanitized(t *testing.T) {
	st := tvShowFlow[0]
	if st.stage != stageAwaitingTitle {
		t.Fatalf("the first step is stage %d, want the title", st.stage)
	}
	if st.skippable {
		t.Error("the title should not be skippable")
	}

	var sess session
	if err := st.assign(&sess, "../../etc"); err != nil {
		t.Fatalf("title rejected when it should not be: %v", err)
	}
	if sess.Title != "_.._etc" {
		t.Errorf("Title = %q, want the sanitized name", sess.Title)
	}

	if err := st.assign(&sess, ".."); err == nil {
		t.Error("a title that leaves nothing usable should be rejected")
	}
}

// The user picks numbers, but what gets stored are the names: the numbering
// only exists in the conversation.
func TestGenreStepResolvesNumbersAgainstTheListShown(t *testing.T) {
	list := []string{"Action", "Sci-Fi", "Noir"}

	tests := []struct {
		name   string
		answer string
		want   string
	}{
		{"one number", "2", "Sci-Fi"},
		{"space separated", "1 3", "Action, Noir"},
		{"comma separated", "1, 3", "Action, Noir"},
		{"repeated", "2 2", "Sci-Fi"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sess := session{Genres: list}
			if err := assignGenres(&sess, tt.answer); err != nil {
				t.Fatalf("%q rejected: %v", tt.answer, err)
			}
			if sess.Genre != tt.want {
				t.Errorf("Genre = %q, want %q", sess.Genre, tt.want)
			}
		})
	}

	// A number outside the list is rejected as a whole: keeping the valid ones
	// and dropping the rest silently loads a genre nobody picked.
	for _, answer := range []string{"0", "4", "sci-fi", "1 4", ""} {
		sess := session{Genres: list}
		if err := assignGenres(&sess, answer); err == nil {
			t.Errorf("%q should be rejected, Genre came out %q", answer, sess.Genre)
		}
	}
}

// If the API did not answer, the list is empty and the step takes free text,
// instead of leaving the upload stuck on a genre.
func TestGenreStepFallsBackToFreeTextWithoutList(t *testing.T) {
	var sess session
	if err := assignGenres(&sess, "sci-fi, noir"); err != nil {
		t.Fatalf("free text rejected: %v", err)
	}
	if sess.Genre != "sci-fi, noir" {
		t.Errorf("Genre = %q, want the text as it came", sess.Genre)
	}
}

// The catalog stores the rating as a number, so loose text cannot get through.
func TestRatingStepTakesNumbersOnly(t *testing.T) {
	st := metadataSteps[4]
	if st.stage != stageAwaitingRating {
		t.Fatalf("step 5 is stage %d, want the rating", st.stage)
	}

	for in, want := range map[string]string{"8.1": "8.1", "8,1": "8.1", "5": "5", "0": "0", "10": "10"} {
		var sess session
		if err := st.assign(&sess, in); err != nil {
			t.Errorf("%q rejected: %v", in, err)
			continue
		}
		if sess.Rating != want {
			t.Errorf("Rating = %q, want %q", sess.Rating, want)
		}
	}

	for _, in := range []string{"", "great", "-1", "11", "8.1.2"} {
		var sess session
		if err := st.assign(&sess, in); err == nil {
			t.Errorf("%q should be rejected, Rating came out %q", in, sess.Rating)
		}
	}
}

func TestValidTimecode(t *testing.T) {
	valid := []string{"0", "90", "123456", "1:30", "0:05", "1:02:03"}
	for _, in := range valid {
		if !validTimecode(in) {
			t.Errorf("%q should be accepted", in)
		}
	}

	invalid := []string{"", ":", "1:", ":30", "1:2", "1:2:3", "90s", "-5", "1:30:00:00", "one:two"}
	for _, in := range invalid {
		if validTimecode(in) {
			t.Errorf("%q should be rejected", in)
		}
	}
}

// changedFields lists the text fields that differ between two sessions.
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
